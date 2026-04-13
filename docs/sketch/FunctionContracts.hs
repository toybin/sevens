-- FunctionContracts.hs
--
-- Sketch of the function-level contract layer. Sits on top of the
-- type kernel (TypesKernel.hs) and the conformance index
-- (ConformanceIndex.hs). Exercises the discuss case end-to-end.
--
-- Run: ~/.ghcup/bin/runghc-9.6 docs/sketch/FunctionContracts.hs
--
-- What this file proves out:
--
--   1. Contract check at dispatch time: a function F with declared
--      input type T can only be applied to a target whose conformance
--      set contains some T' where T' <: T. Primitives accept anything.
--
--   2. Dependent output resolution: a function may pick its output
--      type as a function of (KB, target). The router runs BEFORE the
--      LLM is called, so the LLM is always told exactly one shape.
--
--   3. Schema instruction is derived from the resolved output type.
--      The same type definition drives both (a) what the LLM is told
--      to produce and (b) what the response is validated against.
--      No prose duplication; no drift between prompt and parser.
--
--   4. Parse-time validation with the full refinement chain,
--      including contextual (KB-dependent) refinements. Failures are
--      specific: which clause broke, what was expected, what was
--      observed.
--
--   5. The discuss case: when a discussion exists, output resolves to
--      discussion-turn (edit); when it does not, to discussion-start
--      (create). Malformed LLM responses fail at specific refinement
--      boundaries, not silently downstream.
--
-- This sketch intentionally inlines the kernel definitions it needs
-- (Value, Refinement, validate, schemaInstruction) rather than
-- importing TypesKernel.hs, so runghc can execute it standalone. When
-- we port to Go, the kernel definitions live in one package and this
-- layer imports them.

module Main where

import Data.Map.Strict (Map)
import qualified Data.Map.Strict as Map
import Data.Set (Set)
import qualified Data.Set as Set
import Data.List (isSuffixOf, isPrefixOf, foldl')
import System.Exit (exitFailure, exitSuccess)

--------------------------------------------------------------------------
-- Names
--------------------------------------------------------------------------

newtype TypeName     = TypeName     String deriving (Eq, Ord, Show)
newtype FieldName    = FieldName    String deriving (Eq, Ord, Show)
newtype Title        = Title        String deriving (Eq, Ord, Show)
newtype NodeId       = NodeId       String deriving (Eq, Ord, Show)
newtype FunctionName = FunctionName String deriving (Eq, Ord, Show)

unFieldName :: FieldName -> String
unFieldName (FieldName s) = s

--------------------------------------------------------------------------
-- Kernel (inlined from TypesKernel.hs)
--------------------------------------------------------------------------

data FieldKind = FKString | FKContent | FKExtra deriving (Eq, Show)

data FieldSpec = FieldSpec
  { fsName     :: FieldName
  , fsKind     :: FieldKind
  , fsRequired :: Bool
  } deriving Show

data FieldValue
  = VString String
  | VMap (Map String String)
  | VAbsent
  deriving (Eq, Show)

data Value = Value { valueFields :: Map FieldName FieldValue } deriving (Eq, Show)

mkValue :: [(String, FieldValue)] -> Value
mkValue pairs = Value (Map.fromList [(FieldName k, v) | (k, v) <- pairs])

getField :: FieldName -> Value -> FieldValue
getField fn (Value fs) = Map.findWithDefault VAbsent fn fs

data Primitive = PText | PCreate | PEdit | PSuggestion deriving (Eq, Ord, Show)

primitiveName :: Primitive -> TypeName
primitiveName PText       = TypeName "text"
primitiveName PCreate     = TypeName "create"
primitiveName PEdit       = TypeName "edit"
primitiveName PSuggestion = TypeName "suggestion"

primitiveShape :: Primitive -> [FieldSpec]
primitiveShape PText =
  [ FieldSpec (FieldName "text") FKString True ]
primitiveShape PCreate =
  [ FieldSpec (FieldName "title")   FKString  True
  , FieldSpec (FieldName "parent")  FKString  False
  , FieldSpec (FieldName "content") FKContent True
  , FieldSpec (FieldName "extra")   FKExtra   False
  ]
primitiveShape PEdit =
  [ FieldSpec (FieldName "file")     FKString True
  , FieldSpec (FieldName "old_text") FKString True
  , FieldSpec (FieldName "new_text") FKString True
  ]
primitiveShape PSuggestion =
  [ FieldSpec (FieldName "title")     FKString True
  , FieldSpec (FieldName "rationale") FKString True
  ]

data Refinement
  = Intrinsic  (Value -> Either String ())
  | Contextual (KB -> Value -> Either String ())

data NamedRefinement = NamedRefinement
  { refName  :: String
  , refCheck :: Refinement
  }

data TypeDef
  = PrimT Primitive
  | DerivedT
      { dtName        :: TypeName
      , dtParent      :: TypeName
      , dtExtraFields :: [FieldSpec]
      , dtRefinements :: [NamedRefinement]
      }

typeDefName :: TypeDef -> TypeName
typeDefName (PrimT p)               = primitiveName p
typeDefName (DerivedT { dtName = n }) = n

type Registry = Map TypeName TypeDef

mkRegistry :: [TypeDef] -> Registry
mkRegistry tds =
  foldr (\td reg -> Map.insert (typeDefName td) td reg) Map.empty tds

primitivesRegistry :: Registry
primitivesRegistry = mkRegistry
  [ PrimT PText, PrimT PCreate, PrimT PEdit, PrimT PSuggestion ]

-- Subsumption
ancestors :: Registry -> TypeName -> [TypeName]
ancestors reg name =
  case Map.lookup name reg of
    Nothing        -> []
    Just (PrimT p) -> [primitiveName p]
    Just dt@DerivedT{} -> dtName dt : ancestors reg (dtParent dt)

isSubtype :: Registry -> TypeName -> TypeName -> Bool
isSubtype reg sub super = super `elem` ancestors reg sub

-- Shape composition
composedShape :: Registry -> TypeName -> [FieldSpec]
composedShape reg name =
  case Map.lookup name reg of
    Nothing            -> []
    Just (PrimT p)     -> primitiveShape p
    Just dt@DerivedT{} -> overrideFields (composedShape reg (dtParent dt)) (dtExtraFields dt)

overrideFields :: [FieldSpec] -> [FieldSpec] -> [FieldSpec]
overrideFields old new =
  let newNames = map fsName new
      kept     = filter (\f -> fsName f `notElem` newNames) old
  in kept ++ new

collectRefinements :: Registry -> TypeName -> [NamedRefinement]
collectRefinements reg name =
  case Map.lookup name reg of
    Nothing            -> []
    Just (PrimT _)     -> []
    Just dt@DerivedT{} -> collectRefinements reg (dtParent dt) ++ dtRefinements dt

-- Validation
validate :: Registry -> KB -> TypeName -> Value -> Either String ()
validate reg kb name val = do
  case Map.lookup name reg of
    Nothing -> Left ("unknown type: " ++ show name)
    Just _  -> Right ()
  checkFields (composedShape reg name) val
  mapM_ (runRefinement kb val) (collectRefinements reg name)

checkFields :: [FieldSpec] -> Value -> Either String ()
checkFields specs val = mapM_ checkOne specs
  where
    checkOne f =
      case (fsRequired f, getField (fsName f) val) of
        (True, VAbsent)    -> Left ("field " ++ unFieldName (fsName f) ++ " required but absent")
        (True, VString "") -> Left ("field " ++ unFieldName (fsName f) ++ " required but empty")
        _                  -> Right ()

runRefinement :: KB -> Value -> NamedRefinement -> Either String ()
runRefinement _  val (NamedRefinement n (Intrinsic  f)) =
  case f val    of Left e -> Left (n ++ ": " ++ e); Right () -> Right ()
runRefinement kb val (NamedRefinement n (Contextual f)) =
  case f kb val of Left e -> Left (n ++ ": " ++ e); Right () -> Right ()

-- Schema instruction — derived from the type definition, used as
-- authoritative prompt material.
schemaInstruction :: Registry -> TypeName -> String
schemaInstruction reg name@(TypeName n) =
  let fs   = composedShape reg name
      refs = collectRefinements reg name
      showKind FKString  = "string"
      showKind FKContent = "markdown"
      showKind FKExtra   = "map<string,string>"
      fieldLine f =
        let FieldName fn = fsName f
            req = if fsRequired f then "required" else "optional"
        in "  " ++ fn ++ " : " ++ showKind (fsKind f) ++ " (" ++ req ++ ")"
      header   = "Type: " ++ n ++ "\nFields:\n"
      body     = unlines (map fieldLine fs)
      refBlock = case refs of
        [] -> ""
        _  -> "Constraints:\n" ++ unlines [ "  - " ++ refName r | r <- refs ]
  in header ++ body ++ refBlock

--------------------------------------------------------------------------
-- KB
--------------------------------------------------------------------------

data KB = KB { kbNodes :: Map Title String } deriving Show

resolveNode :: KB -> Title -> Maybe String
resolveNode (KB ns) t = Map.lookup t ns

--------------------------------------------------------------------------
-- Example type registry
--------------------------------------------------------------------------

discussionStartType :: TypeDef
discussionStartType = DerivedT
  { dtName        = TypeName "discussion-start"
  , dtParent      = TypeName "create"
  , dtExtraFields = []
  , dtRefinements =
      [ NamedRefinement "title begins with 'Discussion - '" $
          Intrinsic $ \v ->
            case getField (FieldName "title") v of
              VString t ->
                if "Discussion - " `isPrefixOf` t
                then Right ()
                else Left ("title must start with 'Discussion - ', got " ++ show t)
              _ -> Left "title must be a string"
      ]
  }

discussionTurnType :: TypeDef
discussionTurnType = DerivedT
  { dtName        = TypeName "discussion-turn"
  , dtParent      = TypeName "edit"
  , dtExtraFields = []
  , dtRefinements =
      [ NamedRefinement "file resolves in KB" $
          Contextual $ \kb v ->
            case getField (FieldName "file") v of
              VString f ->
                case resolveNode kb (Title f) of
                  Just _  -> Right ()
                  Nothing -> Left ("file " ++ show f ++ " does not resolve in KB")
              _ -> Left "file must be a string"
      , NamedRefinement "old_text is a suffix of the last line of file" $
          Contextual $ \kb v ->
            case (getField (FieldName "file") v, getField (FieldName "old_text") v) of
              (VString f, VString ot) ->
                case resolveNode kb (Title f) of
                  Just content ->
                    let ls       = lines content
                        lastLine = if null ls then "" else last ls
                    in if ot `isSuffixOf` lastLine
                       then Right ()
                       else Left ("old_text is not a suffix of last line " ++ show lastLine)
                  Nothing -> Left ("file " ++ show f ++ " does not resolve")
              _ -> Left "file and old_text must be strings"
      ]
  }

-- A simple derived type representing a task: extends create with
-- required status + deadline in the extra map.
taskType :: TypeDef
taskType = DerivedT
  { dtName        = TypeName "task"
  , dtParent      = TypeName "create"
  , dtExtraFields = []
  , dtRefinements =
      [ NamedRefinement "extra has status and deadline" $
          Intrinsic $ \v ->
            case getField (FieldName "extra") v of
              VMap m ->
                let missing = filter (\k -> not (Map.member k m)) ["status", "deadline"]
                in if null missing
                   then Right ()
                   else Left ("missing keys: " ++ show missing)
              _ -> Left "extra must be a map"
      ]
  }

exampleRegistry :: Registry
exampleRegistry = foldr (\td reg -> Map.insert (typeDefName td) td reg) primitivesRegistry
  [ discussionStartType, discussionTurnType, taskType ]

--------------------------------------------------------------------------
-- Function layer
--------------------------------------------------------------------------

-- A target is the node we're about to apply a function to. For this
-- sketch the target carries a pre-computed set of type names it
-- Full-conforms to, as would be supplied by the conformance index.
data Target = Target
  { targetId       :: NodeId
  , targetTitle    :: Title
  , targetConforms :: Set TypeName
  } deriving Show

-- OutputPicker models both level 4 (dependent shape) and the trivial
-- static case. The picker runs BEFORE the LLM is called, so the LLM
-- is always told exactly one type to produce.
data OutputPicker
  = StaticOutput TypeName
  | DependentOutput String (KB -> Target -> TypeName)
    -- name is for debugging / audit only; the function is opaque

instance Show OutputPicker where
  show (StaticOutput t)       = "StaticOutput " ++ show t
  show (DependentOutput n _)  = "DependentOutput " ++ show n

data FunctionDef = FunctionDef
  { funcName   :: FunctionName
  , funcInput  :: TypeName
  , funcOutput :: OutputPicker
  } deriving Show

resolveOutput :: FunctionDef -> KB -> Target -> TypeName
resolveOutput fn kb target =
  case funcOutput fn of
    StaticOutput t            -> t
    DependentOutput _ picker  -> picker kb target

-- Contract check: can this function be dispatched to this target?
-- The target must have some conformance type T' where T' <: funcInput.
-- Primitives accept anything (they are the top of their subtree; every
-- value satisfies "is a create" because the primitive is vacuous).
canApply :: Registry -> FunctionDef -> Target -> Either String ()
canApply reg fn target =
  let input = funcInput fn
      isPrim = case Map.lookup input reg of
                 Just (PrimT _) -> True
                 _              -> False
      conforms = Set.toList (targetConforms target)
      match    = any (\t -> isSubtype reg t input) conforms
  in if isPrim
     then Right ()
     else if match
          then Right ()
          else Left $
            "cannot apply " ++ show (funcName fn) ++
            " to target " ++ show (targetTitle target) ++
            ": target does not conform to input type " ++ show input ++
            " (conforms to: " ++ show conforms ++ ")"

-- LLMStub is a deterministic stand-in for the real LLM. It takes the
-- schema instruction, the KB, and the target, and returns a Value.
-- Tests install different stubs to simulate well-formed and
-- malformed responses.
type LLMStub = String -> KB -> Target -> Value

-- runFunction: contract check, resolve output, call LLM, validate.
-- Returns the resolved output type and the validated value on
-- success, or a specific error on failure.
runFunction
  :: Registry -> FunctionDef -> KB -> Target -> LLMStub
  -> Either String (TypeName, Value)
runFunction reg fn kb target llm = do
  canApply reg fn target
  let outputType = resolveOutput fn kb target
  let schema     = schemaInstruction reg outputType
  let raw        = llm schema kb target
  case validate reg kb outputType raw of
    Left e  -> Left ("function " ++ show (funcName fn)
                     ++ " output (" ++ show outputType
                     ++ ") failed validation: " ++ e)
    Right () -> Right (outputType, raw)

--------------------------------------------------------------------------
-- Example functions
--------------------------------------------------------------------------

-- discuss: accepts any node (input = create, the top of that branch),
-- output type depends on whether a matching discussion child exists.
discussFn :: FunctionDef
discussFn = FunctionDef
  { funcName   = FunctionName "discuss"
  , funcInput  = TypeName "create"
  , funcOutput = DependentOutput "hasDiscussionChild?" $ \kb target ->
      let Title t = targetTitle target
      in case resolveNode kb (Title ("Discussion - " ++ t)) of
           Just _  -> TypeName "discussion-turn"
           Nothing -> TypeName "discussion-start"
  }

-- promote: accepts tasks only. Input type is the derived subtype, so
-- canApply requires the target to already conform to task.
promoteFn :: FunctionDef
promoteFn = FunctionDef
  { funcName   = FunctionName "promote"
  , funcInput  = TypeName "task"
  , funcOutput = StaticOutput (TypeName "create")
  }

--------------------------------------------------------------------------
-- LLM stubs
--------------------------------------------------------------------------

-- A well-behaved stub that produces a valid discussion-start for any
-- target. Title prefix enforced by the refinement.
llmGoodStart :: LLMStub
llmGoodStart _ _ target =
  let Title t = targetTitle target
  in mkValue
      [ ("title",   VString ("Discussion - " ++ t))
      , ("parent",  VString t)
      , ("content", VString "# Discussion\n\n**[agent]** opening question")
      ]

-- A well-behaved stub that produces a valid discussion-turn by
-- reading the current last line of the discussion file and using it
-- as old_text verbatim.
llmGoodTurn :: LLMStub
llmGoodTurn _ kb target =
  let Title t = targetTitle target
      file    = "Discussion - " ++ t
  in case resolveNode kb (Title file) of
       Nothing -> mkValue []   -- should not happen
       Just content ->
         let ls       = lines content
             lastLine = if null ls then "" else last ls
         in mkValue
             [ ("file",     VString file)
             , ("old_text", VString lastLine)
             , ("new_text", VString (lastLine ++ "\n\n**[agent]** reply"))
             ]

-- Malformed stub: fabricates an old_text that is not a suffix of the
-- real last line. Should fail the suffix refinement.
llmBadTurnSuffix :: LLMStub
llmBadTurnSuffix _ _ target =
  let Title t = targetTitle target
      file    = "Discussion - " ++ t
  in mkValue
      [ ("file",     VString file)
      , ("old_text", VString "a line that isn't really there")
      , ("new_text", VString "whatever")
      ]

-- Malformed stub: empty file field (the exact runtime failure mode
-- the current Go code fails silently on with untitled.md).
llmBadTurnNoFile :: LLMStub
llmBadTurnNoFile _ _ _ =
  mkValue
    [ ("old_text", VString "whatever")
    , ("new_text", VString "whatever")
    ]

-- Malformed stub: title without the discussion prefix.
llmBadStartPrefix :: LLMStub
llmBadStartPrefix _ _ _ =
  mkValue
    [ ("title",   VString "Not A Discussion")
    , ("content", VString "body")
    ]

-- Stub that never gets called; useful for canApply failures.
llmUnused :: LLMStub
llmUnused _ _ _ = mkValue []

--------------------------------------------------------------------------
-- Test targets and KB
--------------------------------------------------------------------------

-- A KB where "CI/CD Substrate" already has a discussion child, but
-- "Braindump" does not.
sampleKB :: KB
sampleKB = KB $ Map.fromList
  [ ( Title "Discussion - CI/CD Substrate"
    , "# Discussion\n\n**[agent]** First question.\nThe final line here."
    )
  , ( Title "Braindump"
    , "# overview\n\nTop-level node."
    )
  ]

plainTarget :: Title -> Target
plainTarget t = Target
  { targetId       = NodeId (show t)
  , targetTitle    = t
  , targetConforms = Set.empty    -- conforms to nothing specific
  }

taskTarget :: Title -> Target
taskTarget t = Target
  { targetId       = NodeId (show t)
  , targetTitle    = t
  , targetConforms = Set.singleton (TypeName "task")
  }

--------------------------------------------------------------------------
-- Tests
--------------------------------------------------------------------------

data TestResult = Pass String | Fail String String

showTest :: TestResult -> String
showTest (Pass n)   = "PASS  " ++ n
showTest (Fail n r) = "FAIL  " ++ n ++ "\n        " ++ r

expectRight :: Show e => String -> Either e a -> TestResult
expectRight name (Right _) = Pass name
expectRight name (Left e)  = Fail name ("expected Right, got Left: " ++ show e)

expectLeft :: Show a => String -> Either String a -> TestResult
expectLeft name (Left _)  = Pass name
expectLeft name (Right v) = Fail name ("expected Left, got Right: " ++ show v)

expectLeftContaining :: Show a => String -> String -> Either String a -> TestResult
expectLeftContaining name needle (Left e)
  | needle `isInfixOf` e = Pass name
  | otherwise = Fail name ("expected error containing " ++ show needle ++ ", got " ++ show e)
expectLeftContaining name needle (Right v) =
  Fail name ("expected Left (containing " ++ show needle ++ "), got Right: " ++ show v)

isInfixOf :: String -> String -> Bool
isInfixOf needle hay =
  any (isPrefixOfStr needle) (tails hay)
  where
    tails [] = [[]]
    tails xs@(_:rest) = xs : tails rest
    isPrefixOfStr [] _          = True
    isPrefixOfStr _  []         = False
    isPrefixOfStr (n:ns) (h:hs) = n == h && isPrefixOfStr ns hs

expectEq :: (Eq a, Show a) => String -> a -> a -> TestResult
expectEq name actual expected
  | actual == expected = Pass name
  | otherwise = Fail name ("expected " ++ show expected ++ ", got " ++ show actual)

tests :: [TestResult]
tests =
  let reg  = exampleRegistry
      kb   = sampleKB

      -- Targets
      ciCd       = plainTarget (Title "CI/CD Substrate")
      brain      = plainTarget (Title "Braindump")
      aTask      = taskTarget  (Title "Write report")
      notATask   = plainTarget (Title "Random Note")

      -- Contract check
      t01 = expectRight "discuss can apply to plain target (primitive input)"
              (canApply reg discussFn ciCd)
      t02 = expectRight "discuss can apply to task target"
              (canApply reg discussFn aTask)
      t03 = expectRight "promote can apply to task"
              (canApply reg promoteFn aTask)
      t04 = expectLeftContaining
              "promote cannot apply to non-task"
              "does not conform to input type"
              (canApply reg promoteFn notATask)

      -- Dependent output resolution
      t05 = expectEq "discuss on CI/CD (has discussion) -> discussion-turn"
              (resolveOutput discussFn kb ciCd)
              (TypeName "discussion-turn")
      t06 = expectEq "discuss on Braindump (no discussion) -> discussion-start"
              (resolveOutput discussFn kb brain)
              (TypeName "discussion-start")

      -- Schema instruction is derived (smoke check: mentions constraints)
      t07 = let s = schemaInstruction reg (TypeName "discussion-turn")
            in expectEq "schema for discussion-turn includes suffix constraint"
                 ("old_text is a suffix of the last line of file" `isInfixOf` s) True
      t08 = let s = schemaInstruction reg (TypeName "discussion-start")
            in expectEq "schema for discussion-start includes title-prefix constraint"
                 ("title begins with 'Discussion - '" `isInfixOf` s) True

      -- End-to-end: success paths
      t09 = expectRight "discuss + good start on Braindump"
              (runFunction reg discussFn kb brain llmGoodStart)
      t10 = expectRight "discuss + good turn on CI/CD"
              (runFunction reg discussFn kb ciCd llmGoodTurn)

      -- End-to-end: validation failures (the discuss bug class)
      t11 = expectLeftContaining
              "discuss + bad turn (wrong suffix) fails with specific clause"
              "old_text is not a suffix"
              (runFunction reg discussFn kb ciCd llmBadTurnSuffix)
      t12 = expectLeftContaining
              "discuss + bad turn (no file) fails loudly, not as untitled.md"
              "file required but absent"
              (runFunction reg discussFn kb ciCd llmBadTurnNoFile)
      t13 = expectLeftContaining
              "discuss + bad start (wrong title prefix) fails"
              "title must start with 'Discussion - '"
              (runFunction reg discussFn kb brain llmBadStartPrefix)

      -- End-to-end: canApply fails before LLM is ever called
      t14 = expectLeftContaining
              "promote on non-task fails at contract check (LLM not called)"
              "does not conform to input type"
              (runFunction reg promoteFn kb notATask llmUnused)

      -- Primitive-input acceptance
      t15 = expectRight "function accepting create accepts a task target"
              (canApply reg discussFn aTask)

      -- Subsumption at output: discussion-turn <: edit
      t16 = expectEq "discussion-turn <: edit"
              (isSubtype reg (TypeName "discussion-turn") (TypeName "edit")) True
      t17 = expectEq "discussion-start <: create"
              (isSubtype reg (TypeName "discussion-start") (TypeName "create")) True

      -- A valid value of a supertype does NOT pass the subtype
      -- validator (confirming we always validate against the resolved
      -- output, not a parent).
      t18 = let v = mkValue
                     [ ("file",     VString "Discussion - CI/CD Substrate")
                     , ("old_text", VString "not matching")
                     , ("new_text", VString "whatever")
                     ]
            in expectLeft
                 "well-shaped edit fails validation as discussion-turn"
                 (validate reg kb (TypeName "discussion-turn") v)

  in [t01,t02,t03,t04,t05,t06,t07,t08,t09,t10,t11,t12,t13,t14,t15,t16,t17,t18]

main :: IO ()
main = do
  let rs = tests
  mapM_ (putStrLn . showTest) rs
  let failed = [r | r@(Fail _ _) <- rs]
  putStrLn ""
  if null failed
    then do
      putStrLn $ "All " ++ show (length rs) ++ " tests passed."
      putStrLn ""
      putStrLn "--- schemaInstruction(discussion-turn) ---"
      putStrLn (schemaInstruction exampleRegistry (TypeName "discussion-turn"))
      putStrLn "--- schemaInstruction(discussion-start) ---"
      putStrLn (schemaInstruction exampleRegistry (TypeName "discussion-start"))
      exitSuccess
    else do
      putStrLn $ show (length failed) ++ " of " ++ show (length rs) ++ " tests failed."
      exitFailure
