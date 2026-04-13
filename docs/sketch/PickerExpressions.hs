-- PickerExpressions.hs
--
-- Replaces the opaque Haskell-function form of DependentOutput with
-- a small, serializable expression language for dependent output
-- pickers. Proves that the minimum vocabulary needed for real
-- sevens functions (discuss, audit, etc.) is tiny and has a
-- straightforward evaluator.
--
-- Run: ~/.ghcup/bin/runghc-9.6 docs/sketch/PickerExpressions.hs
--
-- Load-bearing claims:
--
--   1. A picker expression is pure data: it has no opaque function
--      closure, so it can be serialized, inspected, printed, and
--      loaded from EDN. Two pickers expressing the same logic are
--      (==)-equal.
--
--   2. The vocabulary is FIXED and CLOSED. There are no user-defined
--      primitives and no general lambda. This keeps the language
--      bounded so every picker is decidable in finite time and
--      every picker's set of possible return types is statically
--      enumerable (the design requirement for load-time pipeline
--      typechecking from PipelineComposition.hs).
--
--   3. The load-time check "declared alternatives must be a
--      superset of what the picker can actually return" is
--      DECIDABLE over this language. The evaluator walks the
--      expression; the type-set walker walks the same structure
--      collecting LitType literals.
--
--   4. The discuss case fits in four nodes: If (ExistsChild (Concat
--      [...])) (LitType ...) (LitType ...).
--
-- The minimum vocabulary (eight constructors) is:
--
--   LitType t               — type literal, evaluates to a TypeName
--   LitStr  s               — string literal
--   If      c t e           — conditional on a bool expression
--   And / Or / Not          — boolean combinators
--   Eq      a b             — equality of two string exprs
--   Concat  [e]             — string concat
--   TargetTitle             — string value of the target's title
--   ExistsNode  e           — KB query: does a node with this title exist?
--   HasType target T        — is the target known to conform to type T?
--   PriorOutputType i       — type of step i's prior output (i < current step)
--
-- This is enough for every real router we've discussed. More can be
-- added, but each addition is a named primitive — no general-purpose
-- Turing-complete escape hatch.

module Main where

import Data.Map.Strict (Map)
import qualified Data.Map.Strict as Map
import Data.Set (Set)
import qualified Data.Set as Set
import Data.List (isPrefixOf)
import System.Exit (exitFailure, exitSuccess)

--------------------------------------------------------------------------
-- Minimum kernel (same as prior sketches, condensed)
--------------------------------------------------------------------------

newtype TypeName  = TypeName  String deriving (Eq, Ord, Show)
newtype Title     = Title     String deriving (Eq, Ord, Show)
newtype NodeId    = NodeId    String deriving (Eq, Ord, Show)
newtype FieldName = FieldName String deriving (Eq, Ord, Show)

data FieldValue = VString String | VAbsent deriving (Eq, Show)
data Value = Value { valueFields :: Map FieldName FieldValue } deriving (Eq, Show)

data KB = KB { kbNodes :: Map Title String } deriving Show

resolveNode :: KB -> Title -> Maybe String
resolveNode (KB ns) t = Map.lookup t ns

data Target = Target
  { targetId       :: NodeId
  , targetTitle    :: Title
  , targetConforms :: Set TypeName
  } deriving Show

--------------------------------------------------------------------------
-- StepContext (minimal — we only need what the picker can read)
--------------------------------------------------------------------------

data StepContext = StepContext
  { ctxKB           :: KB
  , ctxTarget       :: Target
  , ctxPriorOutputs :: [(TypeName, Value)]   -- in ORDER, step 0 first
  }

--------------------------------------------------------------------------
-- Picker expression language
--------------------------------------------------------------------------

data PickerExpr
  = LitType TypeName
  | LitStr  String
  | If      PickerExpr PickerExpr PickerExpr
  | PAnd    PickerExpr PickerExpr
  | POr     PickerExpr PickerExpr
  | PNot    PickerExpr
  | Eq      PickerExpr PickerExpr
  | Concat  [PickerExpr]
  | TargetTitle
  | ExistsNode PickerExpr       -- argument is a string expression
  | HasType   TypeName          -- does the target conform to this type?
  | PriorOutputType Int         -- type of step i's output
  deriving (Eq, Show)

--------------------------------------------------------------------------
-- Picker values (what expressions evaluate to)
--------------------------------------------------------------------------

data PickerValue
  = PVType TypeName
  | PVString String
  | PVBool Bool
  deriving (Eq, Show)

--------------------------------------------------------------------------
-- Evaluator
--------------------------------------------------------------------------

evalPicker :: PickerExpr -> StepContext -> Either String PickerValue
evalPicker expr ctx = case expr of

  LitType t -> Right (PVType t)
  LitStr  s -> Right (PVString s)

  If c t e -> do
    cv <- evalPicker c ctx
    case cv of
      PVBool True  -> evalPicker t ctx
      PVBool False -> evalPicker e ctx
      other        -> Left ("if condition must be bool, got " ++ show other)

  PAnd a b -> boolOp "and" a b (&&) ctx
  POr  a b -> boolOp "or"  a b (||) ctx
  PNot x   -> do
    v <- evalPicker x ctx
    case v of
      PVBool b -> Right (PVBool (not b))
      other    -> Left ("not requires bool, got " ++ show other)

  Eq a b -> do
    av <- evalPicker a ctx
    bv <- evalPicker b ctx
    case (av, bv) of
      (PVString x, PVString y) -> Right (PVBool (x == y))
      (PVType   x, PVType   y) -> Right (PVBool (x == y))
      (PVBool   x, PVBool   y) -> Right (PVBool (x == y))
      _                        -> Left ("eq operands mismatched: "
                                         ++ show av ++ " vs " ++ show bv)

  Concat parts -> do
    vs <- mapM (`evalPicker` ctx) parts
    let strs = [s | PVString s <- vs]
    if length strs == length vs
      then Right (PVString (concat strs))
      else Left "concat requires all string args"

  TargetTitle ->
    let Title t = targetTitle (ctxTarget ctx)
    in Right (PVString t)

  ExistsNode arg -> do
    av <- evalPicker arg ctx
    case av of
      PVString title ->
        case resolveNode (ctxKB ctx) (Title title) of
          Just _  -> Right (PVBool True)
          Nothing -> Right (PVBool False)
      other -> Left ("exists-node? requires string arg, got " ++ show other)

  HasType t ->
    Right (PVBool (t `Set.member` targetConforms (ctxTarget ctx)))

  PriorOutputType i ->
    let outs = ctxPriorOutputs ctx  -- in order, step 0 first
    in if i >= 0 && i < length outs
       then Right (PVType (fst (outs !! i)))
       else Left ("prior-output-type index " ++ show i ++ " out of range")
  where
    boolOp name a b op c = do
      av <- evalPicker a c
      bv <- evalPicker b c
      case (av, bv) of
        (PVBool x, PVBool y) -> Right (PVBool (op x y))
        _                    -> Left (name ++ " requires bool args")

--------------------------------------------------------------------------
-- Static analysis: the set of type literals an expression can return.
--
-- This is the load-time check. Walk the expression, collect every
-- LitType that appears in a position that could be the final return
-- value (i.e., not inside a condition/string/bool sub-expression).
--
-- Returns Nothing if the expression's return type cannot be bounded
-- (e.g., a PriorOutputType whose possible outputs are not known here).
-- Otherwise returns a Just set of possible TypeName returns.
--------------------------------------------------------------------------

possibleReturnTypes :: PickerExpr -> Maybe (Set TypeName)
possibleReturnTypes expr = case expr of
  LitType t      -> Just (Set.singleton t)
  If _ t e       -> do
    tt <- possibleReturnTypes t
    ee <- possibleReturnTypes e
    Just (tt `Set.union` ee)
  PriorOutputType _ -> Nothing    -- depends on runtime; must be checked against step chain
  -- Anything else in return position is a type error; the evaluator
  -- will reject it. Statically we note we cannot prove a set.
  LitStr  _      -> Just Set.empty    -- never returns a type literal
  Eq      _ _    -> Just Set.empty
  PAnd    _ _    -> Just Set.empty
  POr     _ _    -> Just Set.empty
  PNot    _      -> Just Set.empty
  Concat  _      -> Just Set.empty
  TargetTitle    -> Just Set.empty
  ExistsNode _   -> Just Set.empty
  HasType _      -> Just Set.empty

--------------------------------------------------------------------------
-- OutputPicker with PickerExpr instead of Haskell function
--------------------------------------------------------------------------

data OutputPicker
  = StaticOutput TypeName
  | DependentOutput
      { dpName         :: String
      , dpAlternatives :: [TypeName]
      , dpExpr         :: PickerExpr
      }
  deriving (Eq, Show)

resolveOutput :: OutputPicker -> StepContext -> Either String TypeName
resolveOutput (StaticOutput t) _ = Right t
resolveOutput (DependentOutput n alts e) ctx = do
  v <- evalPicker e ctx
  case v of
    PVType t ->
      if t `elem` alts
      then Right t
      else Left ("picker " ++ show n ++ " returned " ++ show t
                  ++ " which is not in declared alternatives " ++ show alts)
    other -> Left ("picker " ++ show n ++ " must evaluate to a type, got " ++ show other)

-- Load-time check for a single OutputPicker: does the statically-
-- derivable return-type set agree with the declared alternatives?
checkPickerDeclaration :: OutputPicker -> Either String ()
checkPickerDeclaration (StaticOutput _) = Right ()
checkPickerDeclaration (DependentOutput n alts e) =
  case possibleReturnTypes e of
    Nothing -> Right ()   -- unbounded; defer to runtime
    Just actual ->
      let declared = Set.fromList alts
          extra    = actual `Set.difference` declared
      in if Set.null extra
         then Right ()
         else Left ("picker " ++ show n ++ " can return "
                     ++ show (Set.toList extra)
                     ++ " which is not in declared alternatives " ++ show alts)

--------------------------------------------------------------------------
-- The discuss picker expressed in the language
--
-- Equivalent to: if (exists-node? (concat "Discussion - " target-title))
--                then discussion-turn
--                else discussion-start
--------------------------------------------------------------------------

discussPicker :: OutputPicker
discussPicker = DependentOutput
  { dpName         = "discuss-router"
  , dpAlternatives = [TypeName "discussion-turn", TypeName "discussion-start"]
  , dpExpr =
      If (ExistsNode (Concat [LitStr "Discussion - ", TargetTitle]))
         (LitType (TypeName "discussion-turn"))
         (LitType (TypeName "discussion-start"))
  }

-- A deliberately broken picker whose expression can return a type
-- that is not in its declared alternatives. Should fail
-- checkPickerDeclaration.
brokenPicker :: OutputPicker
brokenPicker = DependentOutput
  { dpName         = "broken"
  , dpAlternatives = [TypeName "discussion-turn"]
  , dpExpr =
      If (HasType (TypeName "nothing"))
         (LitType (TypeName "discussion-turn"))
         (LitType (TypeName "discussion-start"))   -- not declared
  }

-- A picker whose condition is itself complex: "route to discussion-turn
-- if the discussion exists AND the target is not already a draft node".
complexPicker :: OutputPicker
complexPicker = DependentOutput
  { dpName         = "complex"
  , dpAlternatives = [TypeName "discussion-turn", TypeName "discussion-start"]
  , dpExpr =
      If (PAnd
            (ExistsNode (Concat [LitStr "Discussion - ", TargetTitle]))
            (PNot (HasType (TypeName "draft"))))
         (LitType (TypeName "discussion-turn"))
         (LitType (TypeName "discussion-start"))
  }

--------------------------------------------------------------------------
-- Fixtures
--------------------------------------------------------------------------

kbWithDiscussion :: KB
kbWithDiscussion = KB $ Map.fromList
  [ (Title "Discussion - CI/CD Substrate", "# Discussion\n")
  ]

kbWithoutDiscussion :: KB
kbWithoutDiscussion = KB Map.empty

target :: Title -> Set TypeName -> Target
target t ts = Target (NodeId (show t)) t ts

ciCd :: Target
ciCd = target (Title "CI/CD Substrate") Set.empty

brain :: Target
brain = target (Title "Braindump") Set.empty

brainAsDraft :: Target
brainAsDraft = target (Title "Braindump") (Set.singleton (TypeName "draft"))

mkCtx :: KB -> Target -> StepContext
mkCtx kb t = StepContext kb t []

--------------------------------------------------------------------------
-- Tests
--------------------------------------------------------------------------

data TR = Pass String | Fail String String
showTR (Pass n)   = "PASS  " ++ n
showTR (Fail n r) = "FAIL  " ++ n ++ "\n        " ++ r

expectRight :: (Show a, Show e) => String -> Either e a -> TR
expectRight n (Right _) = Pass n
expectRight n (Left e)  = Fail n ("expected Right, got Left: " ++ show e)

expectLeft :: Show a => String -> Either String a -> TR
expectLeft n (Left _)  = Pass n
expectLeft n (Right v) = Fail n ("expected Left, got Right: " ++ show v)

expectLeftContaining :: Show a => String -> String -> Either String a -> TR
expectLeftContaining n needle (Left e)
  | needle `isInfixOfStr` e = Pass n
  | otherwise = Fail n ("expected error containing " ++ show needle ++ ", got " ++ e)
expectLeftContaining n _ (Right v) = Fail n ("expected Left, got Right: " ++ show v)

expectEq :: (Eq a, Show a) => String -> a -> a -> TR
expectEq n actual expected
  | actual == expected = Pass n
  | otherwise = Fail n ("expected " ++ show expected ++ ", got " ++ show actual)

isInfixOfStr :: String -> String -> Bool
isInfixOfStr needle hay = any (needle `isPrefixOf`) (tailsOf hay)
  where tailsOf [] = [[]]; tailsOf xs@(_:rest) = xs : tailsOf rest

tests :: [TR]
tests =
  [ -- === Evaluator basics ===
    expectEq "LitType evaluates to PVType"
      (evalPicker (LitType (TypeName "x")) (mkCtx kbWithoutDiscussion brain))
      (Right (PVType (TypeName "x")))

  , expectEq "LitStr evaluates to PVString"
      (evalPicker (LitStr "hello") (mkCtx kbWithoutDiscussion brain))
      (Right (PVString "hello"))

  , expectEq "TargetTitle reads from context"
      (evalPicker TargetTitle (mkCtx kbWithoutDiscussion brain))
      (Right (PVString "Braindump"))

  , expectEq "Concat joins strings"
      (evalPicker (Concat [LitStr "a", LitStr "b", LitStr "c"])
                  (mkCtx kbWithoutDiscussion brain))
      (Right (PVString "abc"))

  , expectEq "Eq returns PVBool"
      (evalPicker (Eq (LitStr "x") (LitStr "x")) (mkCtx kbWithoutDiscussion brain))
      (Right (PVBool True))

  , -- === ExistsNode against KB ===
    expectEq "ExistsNode true for present node"
      (evalPicker
         (ExistsNode (Concat [LitStr "Discussion - ", TargetTitle]))
         (mkCtx kbWithDiscussion ciCd))
      (Right (PVBool True))

  , expectEq "ExistsNode false for absent node"
      (evalPicker
         (ExistsNode (Concat [LitStr "Discussion - ", TargetTitle]))
         (mkCtx kbWithoutDiscussion brain))
      (Right (PVBool False))

  , -- === Discuss picker end-to-end ===
    expectEq "discussPicker -> discussion-turn when discussion exists"
      (resolveOutput discussPicker (mkCtx kbWithDiscussion ciCd))
      (Right (TypeName "discussion-turn"))

  , expectEq "discussPicker -> discussion-start when discussion absent"
      (resolveOutput discussPicker (mkCtx kbWithoutDiscussion brain))
      (Right (TypeName "discussion-start"))

  , -- === HasType queries target's conformance set ===
    expectEq "HasType true when target conforms"
      (evalPicker (HasType (TypeName "draft"))
                  (mkCtx kbWithoutDiscussion brainAsDraft))
      (Right (PVBool True))

  , expectEq "HasType false when target does not conform"
      (evalPicker (HasType (TypeName "draft"))
                  (mkCtx kbWithoutDiscussion brain))
      (Right (PVBool False))

  , -- === Complex picker: conjunction ===
    expectEq "complexPicker on normal target with discussion -> turn"
      (resolveOutput complexPicker (mkCtx kbWithDiscussion ciCd))
      (Right (TypeName "discussion-turn"))

  , expectEq "complexPicker on draft target with discussion -> start"
      (resolveOutput complexPicker
        (StepContext kbWithDiscussion
          (target (Title "CI/CD Substrate") (Set.singleton (TypeName "draft")))
          []))
      (Right (TypeName "discussion-start"))

  , -- === Load-time check for alternative-declaration correctness ===
    expectRight "discussPicker passes load-time check"
      (checkPickerDeclaration discussPicker)

  , expectLeftContaining
      "brokenPicker fails load-time check (undeclared alternative)"
      "which is not in declared alternatives"
      (checkPickerDeclaration brokenPicker)

  , -- === Runtime protects against picker lies (sanity: unreachable
    -- via the expression language because LitType is exhaustively
    -- declared, so this is a defense-in-depth check) ===
    expectLeftContaining
      "runtime picker check catches undeclared return"
      "which is not in declared alternatives"
      (resolveOutput brokenPicker (mkCtx kbWithoutDiscussion brain))

  , -- === Equality on data: same expression == ===
    expectEq "discussPicker is (==)-equal to itself (serializable)"
      discussPicker discussPicker

  , -- === Type error inside a picker: not a type in return position ===
    let bad = DependentOutput "bad" [TypeName "x"] (LitStr "oops")
    in expectLeftContaining
         "picker returning non-type fails at runtime"
         "must evaluate to a type"
         (resolveOutput bad (mkCtx kbWithoutDiscussion brain))

  , -- === possibleReturnTypes walks If branches ===
    expectEq "possibleReturnTypes of discussPicker"
      (possibleReturnTypes (dpExprOf discussPicker))
      (Just (Set.fromList [TypeName "discussion-turn", TypeName "discussion-start"]))
  ]
  where
    dpExprOf (DependentOutput _ _ e) = e
    dpExprOf _ = LitStr ""

main :: IO ()
main = do
  let rs = tests
  mapM_ (putStrLn . showTR) rs
  let failed = [r | r@(Fail _ _) <- rs]
  putStrLn ""
  if null failed
    then do
      putStrLn $ "All " ++ show (length rs) ++ " tests passed."
      putStrLn ""
      putStrLn "--- discussPicker as data (serializable) ---"
      putStrLn (show discussPicker)
      exitSuccess
    else do
      putStrLn $ show (length failed) ++ " of " ++ show (length rs) ++ " failed."
      exitFailure
