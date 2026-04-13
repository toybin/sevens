-- PipelineGates.hs
--
-- Adds pause/resume gates to the pipeline model. A gated step, after
-- producing and validating its output, yields control instead of
-- advancing — the caller sees a Suspended result carrying enough
-- state to resume later with Accept / Reject / Revise.
--
-- Run: ~/.ghcup/bin/runghc-9.6 docs/sketch/PipelineGates.hs
--
-- Load-bearing claims proved here:
--
--   1. Gating is orthogonal to type discipline. Every step — gated
--      or not — still runs canDispatch / checkPipeline / validate.
--      A gate cannot let malformed output through; it can only defer
--      the decision about whether to advance.
--
--   2. Suspended state is self-contained. A Suspended value carries
--      (step index, prior outputs, pending review value). Nothing
--      about the earlier steps needs to be re-run on resume.
--
--   3. Accept, Reject, Revise are distinct moves with distinct
--      semantics. Accept commits the pending value and advances.
--      Reject terminates. Revise re-runs the current step with
--      feedback available to the LLM via StepContext.
--
--   4. After a Revise-resume, the type check runs again. You cannot
--      sneak an invalid value past a gate by revising it.

module Main where

import Control.Monad (foldM)
import Data.Map.Strict (Map)
import qualified Data.Map.Strict as Map
import Data.Set (Set)
import qualified Data.Set as Set
import Data.List (isPrefixOf)
import System.Exit (exitFailure, exitSuccess)

--------------------------------------------------------------------------
-- Kernel (inlined; same as earlier sketches)
--------------------------------------------------------------------------

newtype TypeName     = TypeName     String deriving (Eq, Ord, Show)
newtype FieldName    = FieldName    String deriving (Eq, Ord, Show)
newtype Title        = Title        String deriving (Eq, Ord, Show)
newtype NodeId       = NodeId       String deriving (Eq, Ord, Show)
newtype FunctionName = FunctionName String deriving (Eq, Ord, Show)

data FieldKind = FKString | FKContent | FKExtra deriving (Eq, Show)
data FieldSpec = FieldSpec
  { fsName :: FieldName, fsKind :: FieldKind, fsRequired :: Bool } deriving Show

data FieldValue = VString String | VMap (Map String String) | VAbsent
  deriving (Eq, Show)
data Value = Value { valueFields :: Map FieldName FieldValue } deriving (Eq, Show)

mkValue :: [(String, FieldValue)] -> Value
mkValue ps = Value (Map.fromList [(FieldName k, v) | (k, v) <- ps])

getField :: FieldName -> Value -> FieldValue
getField fn (Value fs) = Map.findWithDefault VAbsent fn fs

data Primitive = PText | PCreate | PEdit | PSuggestion deriving (Eq, Ord, Show)

primitiveName :: Primitive -> TypeName
primitiveName PText = TypeName "text"
primitiveName PCreate = TypeName "create"
primitiveName PEdit = TypeName "edit"
primitiveName PSuggestion = TypeName "suggestion"

primitiveShape :: Primitive -> [FieldSpec]
primitiveShape PText = [FieldSpec (FieldName "text") FKString True]
primitiveShape PCreate =
  [ FieldSpec (FieldName "title")   FKString  True
  , FieldSpec (FieldName "content") FKContent True
  , FieldSpec (FieldName "parent")  FKString  False
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

data NamedRefinement = NamedRefinement { refName :: String, refCheck :: Refinement }

data TypeDef
  = PrimT Primitive
  | DerivedT
      { dtName :: TypeName
      , dtParent :: TypeName
      , dtRefinements :: [NamedRefinement]
      }

typeDefName :: TypeDef -> TypeName
typeDefName (PrimT p) = primitiveName p
typeDefName DerivedT{ dtName = n } = n

type Registry = Map TypeName TypeDef

primitivesRegistry :: Registry
primitivesRegistry = foldr (\td r -> Map.insert (typeDefName td) td r) Map.empty
  [ PrimT PText, PrimT PCreate, PrimT PEdit, PrimT PSuggestion ]

ancestors :: Registry -> TypeName -> [TypeName]
ancestors reg name = case Map.lookup name reg of
  Nothing -> []
  Just (PrimT p) -> [primitiveName p]
  Just dt@DerivedT{} -> dtName dt : ancestors reg (dtParent dt)

isSubtype :: Registry -> TypeName -> TypeName -> Bool
isSubtype reg sub super = super `elem` ancestors reg sub

composedShape :: Registry -> TypeName -> [FieldSpec]
composedShape reg name = case Map.lookup name reg of
  Nothing -> []
  Just (PrimT p) -> primitiveShape p
  Just DerivedT{ dtParent = p } -> composedShape reg p

collectRefinements :: Registry -> TypeName -> [NamedRefinement]
collectRefinements reg name = case Map.lookup name reg of
  Nothing -> []
  Just (PrimT _) -> []
  Just dt@DerivedT{} -> collectRefinements reg (dtParent dt) ++ dtRefinements dt

validate :: Registry -> KB -> TypeName -> Value -> Either String ()
validate reg kb name val = do
  case Map.lookup name reg of
    Nothing -> Left ("unknown type: " ++ show name)
    Just _ -> Right ()
  mapM_ checkField (composedShape reg name)
  mapM_ (runRef kb val) (collectRefinements reg name)
  where
    checkField f = case (fsRequired f, getField (fsName f) val) of
      (True, VAbsent) ->
        let FieldName s = fsName f in Left ("field " ++ s ++ " required but absent")
      (True, VString "") ->
        let FieldName s = fsName f in Left ("field " ++ s ++ " required but empty")
      _ -> Right ()
    runRef _ v (NamedRefinement n (Intrinsic f)) = case f v of
      Left e -> Left (n ++ ": " ++ e); Right () -> Right ()
    runRef k v (NamedRefinement n (Contextual f)) = case f k v of
      Left e -> Left (n ++ ": " ++ e); Right () -> Right ()

schemaInstruction :: Registry -> TypeName -> String
schemaInstruction reg name@(TypeName n) =
  "Type: " ++ n ++ " (fields: " ++ show (length (composedShape reg name))
  ++ ", refs: " ++ show (length (collectRefinements reg name)) ++ ")"

--------------------------------------------------------------------------
-- KB
--------------------------------------------------------------------------

data KB = KB { kbNodes :: Map Title String } deriving Show

--------------------------------------------------------------------------
-- Function layer with gates
--------------------------------------------------------------------------

-- StepContext gains a `ctxFeedback` field so that the Revise path can
-- hand user-supplied feedback to the LLM stub on re-run. The field is
-- Nothing on the initial run, Just "..." during a revise.
data StepContext = StepContext
  { ctxKB           :: KB
  , ctxTarget       :: Target
  , ctxPriorOutputs :: [(TypeName, Value)]   -- reverse order
  , ctxFeedback     :: Maybe String
  }

data Target = Target
  { targetId       :: NodeId
  , targetTitle    :: Title
  , targetConforms :: Set TypeName
  }

type LLMStub = String -> StepContext -> Value

-- GatePolicy is the new element. NoGate: advance automatically.
-- Gated: after producing and validating the output, suspend for
-- external review. The policy carries metadata the resumer may care
-- about (revisable? cancelable?) but the pipeline mechanics don't
-- vary on those flags — they're advisory.
data GatePolicy
  = NoGate
  | Gated { gateRevisable :: Bool, gateCancelable :: Bool }
  deriving Show

data OutputPicker = StaticOutput TypeName deriving Show

data Step = Step
  { stepName   :: String
  , stepInput  :: TypeName
  , stepOutput :: OutputPicker
  , stepGate   :: GatePolicy
  , stepLLM    :: LLMStub
  }

data FunctionDef = FunctionDef
  { funcName  :: FunctionName
  , funcSteps :: [Step]
  }

resolveStepOutput :: Step -> TypeName
resolveStepOutput s = case stepOutput s of StaticOutput t -> t

--------------------------------------------------------------------------
-- Pipeline result and resume actions
--------------------------------------------------------------------------

-- Completed: all steps ran to completion, return all outputs.
-- Suspended: paused at a gate; carries state needed to resume.
data PipelineResult
  = Completed [(TypeName, Value)]     -- in order, step 0 first
  | Suspended
      { susStepIdx      :: Int        -- index of the gated step (the one whose value is pending)
      , susPriorOutputs :: [(TypeName, Value)]  -- outputs of steps BEFORE the suspended one, reversed
      , susPendingType  :: TypeName
      , susPendingValue :: Value
      }
  deriving Show

data Resume = Accept | Reject | Revise String deriving Show

--------------------------------------------------------------------------
-- Load-time + dispatch checks (same shape as PipelineComposition.hs)
--------------------------------------------------------------------------

canDispatch :: Registry -> FunctionDef -> Target -> Either String ()
canDispatch reg fn target = case funcSteps fn of
  [] -> Left "empty function"
  (s0:_) ->
    let input = stepInput s0
        isPrim = case Map.lookup input reg of Just (PrimT _) -> True; _ -> False
        conforms = Set.toList (targetConforms target)
        match = any (\t -> isSubtype reg t input) conforms
    in if isPrim || match
       then Right ()
       else Left ("cannot dispatch " ++ show (funcName fn))

checkPipeline :: Registry -> FunctionDef -> Either String ()
checkPipeline reg fn = case funcSteps fn of
  [] -> Left "empty function"
  ss -> walk ss
  where
    walk [] = Right ()
    walk [_] = Right ()
    walk (a:b:rest) =
      let out = resolveStepOutput a
      in if isSubtype reg out (stepInput b)
         then walk (b:rest)
         else Left ("step " ++ stepName a ++ " output " ++ show out
                     ++ " not <: " ++ stepName b ++ " input " ++ show (stepInput b))

--------------------------------------------------------------------------
-- Running a single step: returns validated ctx or a Suspended signal
--------------------------------------------------------------------------

data StepOutcome
  = StepAdvanced StepContext
  | StepPaused StepContext TypeName Value   -- (ctx AT gate, pending type, pending value)

applyStep :: Registry -> Step -> StepContext -> Either String StepOutcome
applyStep reg s ctx = do
  let outType = resolveStepOutput s
  let schema  = schemaInstruction reg outType
  let raw     = stepLLM s schema ctx
  case validate reg (ctxKB ctx) outType raw of
    Left e -> Left ("step " ++ stepName s ++ " (" ++ show outType ++ "): " ++ e)
    Right () -> case stepGate s of
      NoGate ->
        Right (StepAdvanced
                 ctx { ctxPriorOutputs = (outType, raw) : ctxPriorOutputs ctx
                     , ctxFeedback = Nothing    -- consume any feedback after a successful run
                     })
      Gated{} ->
        Right (StepPaused ctx outType raw)

--------------------------------------------------------------------------
-- Running a pipeline: fold with short-circuit on Suspended
--------------------------------------------------------------------------

runPipeline :: Registry -> FunctionDef -> KB -> Target -> Either String PipelineResult
runPipeline reg fn kb target = do
  checkPipeline reg fn
  canDispatch reg fn target
  let ctx0 = StepContext kb target [] Nothing
  runFrom reg (funcSteps fn) 0 ctx0

-- runFrom: run steps starting at index `idx` against the given ctx.
-- Used both for initial run and for resume (where idx > 0).
runFrom :: Registry -> [Step] -> Int -> StepContext -> Either String PipelineResult
runFrom _   []     _   ctx = Right (Completed (reverse (ctxPriorOutputs ctx)))
runFrom reg (s:ss) idx ctx = do
  outcome <- applyStep reg s ctx
  case outcome of
    StepAdvanced ctx' -> runFrom reg ss (idx + 1) ctx'
    StepPaused  ctx' pt pv ->
      Right (Suspended idx (ctxPriorOutputs ctx') pt pv)

--------------------------------------------------------------------------
-- Resume
--
-- Accept:  commit pending value and run the remaining steps.
-- Reject:  terminate with a terminal error.
-- Revise:  re-run the gated step with feedback in the StepContext.
--          After re-run the gate fires again (so revises can stack)
--          or the user can Accept the new output.
--------------------------------------------------------------------------

resumePipeline
  :: Registry -> FunctionDef -> KB -> Target
  -> PipelineResult -> Resume
  -> Either String PipelineResult
resumePipeline _ _ _ _ (Completed _) _ = Left "already completed"
resumePipeline reg fn kb target (Suspended idx priors pt pv) resume =
  let steps    = funcSteps fn
      thisStep = steps !! idx
      restAfter = drop (idx + 1) steps
      -- Context AT the gate, not including the pending value yet.
      ctxAtGate = StepContext kb target priors Nothing
      -- Context AFTER accepting, includes the pending value.
      ctxAccepted = StepContext kb target ((pt, pv) : priors) Nothing
  in case resume of
       Accept ->
         runFrom reg restAfter (idx + 1) ctxAccepted
       Reject ->
         if case stepGate thisStep of
              Gated _ c -> c
              NoGate    -> False
         then Left ("pipeline rejected at step " ++ stepName thisStep)
         else Left ("step " ++ stepName thisStep ++ " is not cancelable")
       Revise feedback ->
         if case stepGate thisStep of
              Gated r _ -> r
              NoGate    -> False
         then do
           let ctxRevise = ctxAtGate { ctxFeedback = Just feedback }
           -- Re-run this step. Type discipline holds: validate runs
           -- again on the new output. If invalid, Left propagates.
           outcome <- applyStep reg thisStep ctxRevise
           case outcome of
             StepAdvanced ctx' ->
               runFrom reg restAfter (idx + 1) ctx'
             StepPaused ctx' pt' pv' ->
               Right (Suspended idx (ctxPriorOutputs ctx') pt' pv')
         else Left ("step " ++ stepName thisStep ++ " is not revisable")

--------------------------------------------------------------------------
-- Example
--------------------------------------------------------------------------

exampleRegistry :: Registry
exampleRegistry = primitivesRegistry

-- A two-step pipeline: step 0 produces a draft text (gated), step 1
-- produces a finalized text (ungated).
draftAndFinalize :: FunctionDef
draftAndFinalize = FunctionDef
  { funcName = FunctionName "draft-and-finalize"
  , funcSteps =
      [ Step
          { stepName   = "draft"
          , stepInput  = TypeName "create"
          , stepOutput = StaticOutput (TypeName "text")
          , stepGate   = Gated { gateRevisable = True, gateCancelable = True }
          , stepLLM    = \_ ctx ->
              case ctxFeedback ctx of
                Nothing ->
                  mkValue [ ("text", VString "draft v1") ]
                Just fb ->
                  mkValue [ ("text", VString ("draft v2 (revised: " ++ fb ++ ")")) ]
          }
      , Step
          { stepName   = "finalize"
          , stepInput  = TypeName "text"
          , stepOutput = StaticOutput (TypeName "text")
          , stepGate   = NoGate
          , stepLLM    = \_ ctx ->
              let priorText = case ctxPriorOutputs ctx of
                    ((_, v) : _) -> case getField (FieldName "text") v of
                      VString s -> s
                      _         -> "?"
                    _ -> "?"
              in mkValue [ ("text", VString ("FINAL: " ++ priorText)) ]
          }
      ]
  }

-- An un-revisable gated step. Any Revise resume should fail.
nonRevisable :: FunctionDef
nonRevisable = FunctionDef
  { funcName = FunctionName "non-revisable"
  , funcSteps =
      [ Step
          { stepName   = "step"
          , stepInput  = TypeName "create"
          , stepOutput = StaticOutput (TypeName "text")
          , stepGate   = Gated { gateRevisable = False, gateCancelable = True }
          , stepLLM    = \_ _ -> mkValue [("text", VString "fixed")]
          }
      ]
  }

-- A function whose gated step initially produces an INVALID value
-- (empty text). Validation must short-circuit before the gate fires.
badGatedValue :: FunctionDef
badGatedValue = FunctionDef
  { funcName = FunctionName "bad-gated"
  , funcSteps =
      [ Step
          { stepName   = "bad"
          , stepInput  = TypeName "create"
          , stepOutput = StaticOutput (TypeName "text")
          , stepGate   = Gated { gateRevisable = True, gateCancelable = True }
          , stepLLM    = \_ _ -> mkValue [("text", VString "")]   -- required but empty
          }
      ]
  }

sampleTarget :: Target
sampleTarget = Target
  { targetId       = NodeId "t"
  , targetTitle    = Title "Thing"
  , targetConforms = Set.empty
  }

sampleKB :: KB
sampleKB = KB Map.empty

--------------------------------------------------------------------------
-- Tests
--------------------------------------------------------------------------

data TR = Pass String | Fail String String
showTR (Pass n)   = "PASS  " ++ n
showTR (Fail n r) = "FAIL  " ++ n ++ "\n        " ++ r

expectRight :: Show e => String -> Either e a -> TR
expectRight n (Right _) = Pass n
expectRight n (Left e)  = Fail n ("expected Right, got Left: " ++ show e)

expectLeftContaining :: Show a => String -> String -> Either String a -> TR
expectLeftContaining n needle (Left e)
  | needle `isInfixOfStr` e = Pass n
  | otherwise = Fail n ("expected error containing " ++ show needle ++ ", got " ++ show e)
expectLeftContaining n _ (Right v) = Fail n ("expected Left, got Right: " ++ show v)

isInfixOfStr :: String -> String -> Bool
isInfixOfStr needle hay = any (needle `isPrefixOf`) (tailsOf hay)
  where tailsOf [] = [[]]; tailsOf xs@(_:rest) = xs : tailsOf rest

isSuspended :: Either String PipelineResult -> Bool
isSuspended (Right (Suspended{})) = True
isSuspended _ = False

isCompletedWithText :: String -> Either String PipelineResult -> Bool
isCompletedWithText expected (Right (Completed outs)) =
  case outs of
    [_, (_, v)] -> case getField (FieldName "text") v of
      VString s -> s == expected
      _ -> False
    _ -> False
isCompletedWithText _ _ = False

tests :: [TR]
tests =
  let reg = exampleRegistry
      kb  = sampleKB
      tgt = sampleTarget

      -- Initial run: hits the gate after step 0
      initial = runPipeline reg draftAndFinalize kb tgt

      -- Accept path: final output is "FINAL: draft v1"
      acceptOutcome = case initial of
        Right sus -> resumePipeline reg draftAndFinalize kb tgt sus Accept
        Left e    -> Left e

      -- Reject path: terminates with a specific error
      rejectOutcome = case initial of
        Right sus -> resumePipeline reg draftAndFinalize kb tgt sus Reject
        Left e    -> Left e

      -- Revise path: re-run step 0 with feedback, then gate again
      reviseOutcome = case initial of
        Right sus -> resumePipeline reg draftAndFinalize kb tgt sus (Revise "more pith")
        Left e    -> Left e

      -- After revise, accept the v2 draft -> final = "FINAL: draft v2 ..."
      reviseAcceptOutcome = case reviseOutcome of
        Right sus@Suspended{} -> resumePipeline reg draftAndFinalize kb tgt sus Accept
        other                 -> other

      -- Non-revisable gate: Revise should be refused
      nonRevisableInitial = runPipeline reg nonRevisable kb tgt
      nonRevisableRevise = case nonRevisableInitial of
        Right sus -> resumePipeline reg nonRevisable kb tgt sus (Revise "nope")
        Left e    -> Left e

      -- Bad gated value: validation runs BEFORE the gate is reached;
      -- we should get a validation failure, not a Suspended.
      badInitial = runPipeline reg badGatedValue kb tgt
  in
    [ expectRight "initial run reaches the gate" initial
    , if isSuspended initial then Pass "initial run is Suspended (not Completed)"
                             else Fail "initial run is Suspended" "got something else"

    , if isCompletedWithText "FINAL: draft v1" acceptOutcome
        then Pass "Accept -> Completed with 'FINAL: draft v1'"
        else Fail "Accept -> Completed with 'FINAL: draft v1'" (show acceptOutcome)

    , expectLeftContaining "Reject -> terminal error" "rejected" rejectOutcome

    , if isSuspended reviseOutcome
        then Pass "Revise -> re-pauses at the same gate with revised value"
        else Fail "Revise -> re-pauses at the same gate" (show reviseOutcome)

    , if isCompletedWithText "FINAL: draft v2 (revised: more pith)" reviseAcceptOutcome
        then Pass "Revise then Accept -> final uses revised draft"
        else Fail "Revise then Accept -> final uses revised draft" (show reviseAcceptOutcome)

    , expectLeftContaining "non-revisable gate rejects Revise"
        "not revisable" nonRevisableRevise

    , expectLeftContaining "invalid value short-circuits BEFORE the gate"
        "required but empty" badInitial
    ]

main :: IO ()
main = do
  let rs = tests
  mapM_ (putStrLn . showTR) rs
  let failed = [r | r@(Fail _ _) <- rs]
  putStrLn ""
  if null failed
    then do putStrLn $ "All " ++ show (length rs) ++ " tests passed."; exitSuccess
    else do putStrLn $ show (length failed) ++ " of " ++ show (length rs) ++ " failed."; exitFailure
