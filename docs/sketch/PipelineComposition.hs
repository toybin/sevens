-- PipelineComposition.hs
--
-- Sketch of multi-step function composition. Extends FunctionContracts.hs
-- to support pipelines of steps where each step consumes the previous
-- step's output. Exercises the decompose-like case (node -> suggestion
-- -> create) and the load-time type-check for step chaining.
--
-- Run: ~/.ghcup/bin/runghc-9.6 docs/sketch/PipelineComposition.hs
--
-- The load-bearing idea:
--
--   A sevens function is a non-empty list of Steps, each with an
--   input type and an output type (static or dependent). At LOAD
--   time, the chain is verified: for every adjacent pair (s_i, s_{i+1}),
--   every possible output of s_i must be a subtype of s_{i+1}.input.
--   Dependent outputs must declare their set of possible resolutions
--   so the chain can be type-checked without running the picker.
--
--   At RUN time, the pipeline is a foldM (Either-threaded fold) over
--   the step list. Each step resolves its output type, renders a
--   schema instruction, calls the LLM, validates the response, and
--   appends the (type, value) pair to the step context. If any step
--   fails, the whole pipeline short-circuits with a specific error.
--
--   This is literal Haskell function composition: `runPipeline reg fn
--   kb target` is `foldM applyStep ctx0 (funcSteps fn)` with the
--   kernel validator threaded through Either. The curry-friendliness
--   we're proving out is that each step is a pure function of
--   (Registry, Step, StepContext) and the sequencing is just
--   monadic bind.

module Main where

import Control.Monad (foldM, unless)
import Data.Map.Strict (Map)
import qualified Data.Map.Strict as Map
import Data.Set (Set)
import qualified Data.Set as Set
import Data.List (isSuffixOf, isPrefixOf)
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
-- Kernel (inlined; see TypesKernel.hs)
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
mkRegistry = foldr (\td r -> Map.insert (typeDefName td) td r) Map.empty

primitivesRegistry :: Registry
primitivesRegistry = mkRegistry
  [ PrimT PText, PrimT PCreate, PrimT PEdit, PrimT PSuggestion ]

ancestors :: Registry -> TypeName -> [TypeName]
ancestors reg name =
  case Map.lookup name reg of
    Nothing        -> []
    Just (PrimT p) -> [primitiveName p]
    Just dt@DerivedT{} -> dtName dt : ancestors reg (dtParent dt)

isSubtype :: Registry -> TypeName -> TypeName -> Bool
isSubtype reg sub super = super `elem` ancestors reg sub

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
-- Example types
--------------------------------------------------------------------------

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

-- A "dimension" is a specific kind of suggestion produced by the
-- first step of a decompose-like pipeline. It just narrows the name
-- for the demo.
dimensionType :: TypeDef
dimensionType = DerivedT
  { dtName        = TypeName "dimension"
  , dtParent      = TypeName "suggestion"
  , dtExtraFields = []
  , dtRefinements =
      [ NamedRefinement "rationale at least 10 chars" $
          Intrinsic $ \v ->
            case getField (FieldName "rationale") v of
              VString s ->
                if length s >= 10
                then Right ()
                else Left ("rationale too short: " ++ show (length s) ++ " chars")
              _ -> Left "rationale must be a string"
      ]
  }

-- A child-of-dimension: a `create` constrained to carry the parent
-- title field pointing at a real node. Used as the second step's output.
dimensionChildType :: TypeDef
dimensionChildType = DerivedT
  { dtName        = TypeName "dimension-child"
  , dtParent      = TypeName "create"
  , dtExtraFields = []
  , dtRefinements =
      [ NamedRefinement "parent field resolves to a node in KB" $
          Contextual $ \kb v ->
            case getField (FieldName "parent") v of
              VString p ->
                case resolveNode kb (Title p) of
                  Just _  -> Right ()
                  Nothing -> Left ("parent " ++ show p ++ " does not resolve")
              _ -> Left "parent must be a string"
      ]
  }

exampleRegistry :: Registry
exampleRegistry =
  foldr (\td reg -> Map.insert (typeDefName td) td reg) primitivesRegistry
    [ taskType, dimensionType, dimensionChildType ]

--------------------------------------------------------------------------
-- Targets
--------------------------------------------------------------------------

data Target = Target
  { targetId       :: NodeId
  , targetTitle    :: Title
  , targetConforms :: Set TypeName
  } deriving Show

--------------------------------------------------------------------------
-- Function layer (extended with pipelines)
--------------------------------------------------------------------------

-- StepContext is what each step receives: KB, the original target,
-- and the outputs of all prior steps in reverse order (most recent
-- first). A single-step function has an empty prior-output list.
data StepContext = StepContext
  { ctxKB           :: KB
  , ctxTarget       :: Target
  , ctxPriorOutputs :: [(TypeName, Value)]   -- reverse order
  }

emptyCtx :: KB -> Target -> StepContext
emptyCtx kb tgt = StepContext kb tgt []

priorOutput :: StepContext -> Maybe (TypeName, Value)
priorOutput ctx = case ctxPriorOutputs ctx of
  (p:_) -> Just p
  _     -> Nothing

-- OutputPicker now carries a list of declared alternatives so that
-- multi-step pipelines can be type-checked at LOAD time even when a
-- step's output is dependent on runtime KB state.
data OutputPicker
  = StaticOutput TypeName
  | DependentOutput
      { dpName         :: String
      , dpAlternatives :: [TypeName]
      , dpPicker       :: StepContext -> TypeName
      }

possibleOutputs :: OutputPicker -> [TypeName]
possibleOutputs (StaticOutput t)         = [t]
possibleOutputs (DependentOutput _ alts _) = alts

instance Show OutputPicker where
  show (StaticOutput t)           = "StaticOutput " ++ show t
  show (DependentOutput n alts _) = "DependentOutput " ++ show n ++ " " ++ show alts

-- LLMStub is the deterministic stand-in for the real LLM. It takes
-- the composed schema instruction and the StepContext.
type LLMStub = String -> StepContext -> Value

data Step = Step
  { stepName   :: String
  , stepInput  :: TypeName
  , stepOutput :: OutputPicker
  , stepLLM    :: LLMStub
  }

data FunctionDef = FunctionDef
  { funcName  :: FunctionName
  , funcSteps :: [Step]     -- nonempty
  }

--------------------------------------------------------------------------
-- Contract check: target dispatch
--------------------------------------------------------------------------

-- The first step's input is the dispatch type. Contract-checked
-- against the target's conformance set.
canDispatch :: Registry -> FunctionDef -> Target -> Either String ()
canDispatch reg fn target =
  case funcSteps fn of
    []     -> Left "empty function: no steps"
    (s0:_) ->
      let input = stepInput s0
          isPrim = case Map.lookup input reg of
                     Just (PrimT _) -> True
                     _              -> False
          conforms = Set.toList (targetConforms target)
          match    = any (\t -> isSubtype reg t input) conforms
      in if isPrim || match
         then Right ()
         else Left $
           "cannot dispatch " ++ show (funcName fn) ++
           " to " ++ show (targetTitle target) ++
           ": first step input " ++ show input ++
           " not satisfied by target conformance " ++ show conforms

--------------------------------------------------------------------------
-- Load-time pipeline type check
--------------------------------------------------------------------------

-- For every adjacent (s_i, s_{i+1}) pair, verify that EVERY possible
-- output of s_i is a subtype of s_{i+1}.input. Dependent outputs
-- declare their set of alternatives up-front so this check is
-- decidable at load time.
checkPipeline :: Registry -> FunctionDef -> Either String ()
checkPipeline reg fn =
  case funcSteps fn of
    [] -> Left ("function " ++ show (funcName fn) ++ " has no steps")
    _  -> walk (funcSteps fn)
  where
    walk []           = Right ()
    walk [_]          = Right ()
    walk (s1:s2:rest) = do
      let outs = possibleOutputs (stepOutput s1)
      mapM_ (checkPair s1 s2) outs
      walk (s2:rest)

    checkPair s1 s2 outT =
      if isSubtype reg outT (stepInput s2)
      then Right ()
      else Left $
        "pipeline type error: step " ++ show (stepName s1)
        ++ " output " ++ show outT
        ++ " is not a subtype of step " ++ show (stepName s2)
        ++ " input " ++ show (stepInput s2)

--------------------------------------------------------------------------
-- Runtime: resolve step output, with declared-alternative check
--------------------------------------------------------------------------

resolveStepOutput :: Step -> StepContext -> Either String TypeName
resolveStepOutput s ctx =
  case stepOutput s of
    StaticOutput t -> Right t
    DependentOutput n alts picker ->
      let picked = picker ctx
      in if picked `elem` alts
         then Right picked
         else Left $
           "picker " ++ show n ++ " returned " ++ show picked
           ++ " which is not in declared alternatives " ++ show alts

--------------------------------------------------------------------------
-- Runtime: runPipeline as a foldM over the step list
--
-- This is the curried composition point. Each step is a function of
-- (Registry, KB, Step) -> StepContext -> Either String StepContext.
-- Sequencing is monadic bind over Either, which short-circuits on
-- the first failure.
--------------------------------------------------------------------------

runPipeline
  :: Registry -> FunctionDef -> KB -> Target
  -> Either String [(TypeName, Value)]    -- in order, step 0 first
runPipeline reg fn kb target = do
  checkPipeline reg fn
  canDispatch reg fn target
  final <- foldM (applyStep reg kb) (emptyCtx kb target) (funcSteps fn)
  Right (reverse (ctxPriorOutputs final))

applyStep
  :: Registry -> KB -> StepContext -> Step
  -> Either String StepContext
applyStep reg kb ctx s = do
  outType <- resolveStepOutput s ctx
  let schema = schemaInstruction reg outType
  let raw    = stepLLM s schema ctx
  case validate reg kb outType raw of
    Left e  -> Left $
      "step " ++ show (stepName s) ++ " produced invalid "
      ++ show outType ++ ": " ++ e
    Right () -> Right $
      ctx { ctxPriorOutputs = (outType, raw) : ctxPriorOutputs ctx }

--------------------------------------------------------------------------
-- Example functions and stubs
--------------------------------------------------------------------------

-- A simple single-step function to confirm the multi-step machinery
-- also handles n=1.
noticeFn :: FunctionDef
noticeFn = FunctionDef
  { funcName  = FunctionName "notice"
  , funcSteps =
      [ Step
          { stepName   = "observe"
          , stepInput  = TypeName "create"
          , stepOutput = StaticOutput (TypeName "text")
          , stepLLM    = \_ _ -> mkValue [("text", VString "an observation")]
          }
      ]
  }

-- Two-step decompose-like function:
--   step "suggest":  create -> dimension  (suggestion subtype)
--   step "generate": dimension -> dimension-child  (create subtype)
--
-- The step-input type of step 2 is `dimension`, and the step-output
-- of step 1 is `dimension`, so checkPipeline succeeds.
decomposeFn :: FunctionDef
decomposeFn = FunctionDef
  { funcName  = FunctionName "decompose"
  , funcSteps =
      [ Step
          { stepName   = "suggest"
          , stepInput  = TypeName "create"
          , stepOutput = StaticOutput (TypeName "dimension")
          , stepLLM    = \_ ctx ->
              let Title t = targetTitle (ctxTarget ctx)
              in mkValue
                  [ ("title",     VString ("Facet of " ++ t))
                  , ("rationale", VString "A dimension worth exploring further.")
                  ]
          }
      , Step
          { stepName   = "generate"
          , stepInput  = TypeName "dimension"
          , stepOutput = StaticOutput (TypeName "dimension-child")
          , stepLLM    = \_ ctx ->
              let Title parent = targetTitle (ctxTarget ctx)
                  priorTitle = case priorOutput ctx of
                    Just (_, pv) -> case getField (FieldName "title") pv of
                      VString s -> s
                      _         -> "unknown"
                    Nothing -> "unknown"
              in mkValue
                  [ ("title",   VString priorTitle)
                  , ("parent",  VString parent)
                  , ("content", VString ("Generated from " ++ priorTitle))
                  ]
          }
      ]
  }

-- An ill-typed pipeline: step 1 produces text, step 2 expects dimension.
-- text </: dimension, so checkPipeline must reject at LOAD time.
illTypedFn :: FunctionDef
illTypedFn = FunctionDef
  { funcName  = FunctionName "ill-typed"
  , funcSteps =
      [ Step
          { stepName   = "first"
          , stepInput  = TypeName "create"
          , stepOutput = StaticOutput (TypeName "text")
          , stepLLM    = \_ _ -> mkValue [("text", VString "hi")]
          }
      , Step
          { stepName   = "second"
          , stepInput  = TypeName "dimension"     -- wrong: text </: dimension
          , stepOutput = StaticOutput (TypeName "text")
          , stepLLM    = \_ _ -> mkValue [("text", VString "hi")]
          }
      ]
  }

-- A dependent-output pipeline where ALL alternatives satisfy the
-- next step's input. Demonstrates that load-time check can handle
-- DependentOutput when the alternatives are declared.
dependentOKFn :: FunctionDef
dependentOKFn = FunctionDef
  { funcName  = FunctionName "dep-ok"
  , funcSteps =
      [ Step
          { stepName   = "pick"
          , stepInput  = TypeName "create"
          , stepOutput = DependentOutput
              { dpName         = "always-dimension"
              , dpAlternatives = [TypeName "dimension"]
              , dpPicker       = const (TypeName "dimension")
              }
          , stepLLM    = \_ _ -> mkValue
              [ ("title",     VString "x")
              , ("rationale", VString "sufficiently long rationale")
              ]
          }
      , Step
          { stepName   = "consume"
          , stepInput  = TypeName "suggestion"
          , stepOutput = StaticOutput (TypeName "text")
          , stepLLM    = \_ _ -> mkValue [("text", VString "done")]
          }
      ]
  }

-- A dependent-output pipeline where ONE alternative violates the
-- next step's input. Must fail checkPipeline.
dependentBadFn :: FunctionDef
dependentBadFn = FunctionDef
  { funcName  = FunctionName "dep-bad"
  , funcSteps =
      [ Step
          { stepName   = "pick"
          , stepInput  = TypeName "create"
          , stepOutput = DependentOutput
              { dpName         = "dimension-or-text"
              , dpAlternatives = [TypeName "dimension", TypeName "text"]
              , dpPicker       = const (TypeName "dimension")
              }
          , stepLLM    = \_ _ -> mkValue []
          }
      , Step
          { stepName   = "consume"
          , stepInput  = TypeName "suggestion"
          , stepOutput = StaticOutput (TypeName "text")
          , stepLLM    = \_ _ -> mkValue []
          }
      ]
  }

-- A well-typed pipeline whose step-1 LLM returns an invalid value
-- (short rationale). Used to prove runtime validation short-circuits
-- before step 2 runs.
decomposeBadStep1 :: FunctionDef
decomposeBadStep1 = decomposeFn
  { funcSteps =
      [ (head (funcSteps decomposeFn))
          { stepLLM = \_ _ -> mkValue
              [ ("title",     VString "x")
              , ("rationale", VString "short")
              ]
          }
      , (funcSteps decomposeFn !! 1)
      ]
  }

-- A well-typed pipeline whose step-2 LLM produces an invalid value
-- (parent does not resolve). Used to prove validation runs per step.
decomposeBadStep2 :: FunctionDef
decomposeBadStep2 = decomposeFn
  { funcSteps =
      [ head (funcSteps decomposeFn)
      , (funcSteps decomposeFn !! 1)
          { stepLLM = \_ ctx ->
              let priorTitle = case priorOutput ctx of
                    Just (_, pv) -> case getField (FieldName "title") pv of
                      VString s -> s
                      _         -> "unknown"
                    Nothing -> "unknown"
              in mkValue
                  [ ("title",   VString priorTitle)
                  , ("parent",  VString "Does Not Exist In KB")
                  , ("content", VString "body")
                  ]
          }
      ]
  }

-- A pipeline whose dependent picker returns an undeclared alternative
-- (bug in the picker). Runtime should flag it.
pickerLyingFn :: FunctionDef
pickerLyingFn = FunctionDef
  { funcName  = FunctionName "picker-lies"
  , funcSteps =
      [ Step
          { stepName   = "only-step"
          , stepInput  = TypeName "create"
          , stepOutput = DependentOutput
              { dpName         = "claims-dimension-returns-text"
              , dpAlternatives = [TypeName "dimension"]
              , dpPicker       = const (TypeName "text")
              }
          , stepLLM    = \_ _ -> mkValue []
          }
      ]
  }

--------------------------------------------------------------------------
-- Sample KB and targets
--------------------------------------------------------------------------

sampleKB :: KB
sampleKB = KB $ Map.fromList
  [ ( Title "Braindump", "# overview\n\nTop-level node." )
  ]

braindump :: Target
braindump = Target
  { targetId       = NodeId "braindump"
  , targetTitle    = Title "Braindump"
  , targetConforms = Set.empty
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

expectLeftContaining :: Show a => String -> String -> Either String a -> TestResult
expectLeftContaining name needle (Left e)
  | needle `isInfixOfStr` e = Pass name
  | otherwise = Fail name ("expected error containing " ++ show needle ++ ", got " ++ show e)
expectLeftContaining name needle (Right v) =
  Fail name ("expected Left (containing " ++ show needle ++ "), got Right: " ++ show v)

expectEqInt :: String -> Int -> Int -> TestResult
expectEqInt name actual expected
  | actual == expected = Pass name
  | otherwise = Fail name ("expected " ++ show expected ++ ", got " ++ show actual)

isInfixOfStr :: String -> String -> Bool
isInfixOfStr needle hay = any (needle `isPrefixOf`) (tailsStr hay)
  where
    tailsStr []          = [[]]
    tailsStr xs@(_:rest) = xs : tailsStr rest

tests :: [TestResult]
tests =
  let reg = exampleRegistry
      kb  = sampleKB
  in
    [ -- Load-time pipeline checks
      expectRight "notice pipeline type-checks"
        (checkPipeline reg noticeFn)

    , expectRight "decompose pipeline type-checks (dimension <: suggestion)"
        (checkPipeline reg decomposeFn)

    , expectLeftContaining
        "ill-typed pipeline fails checkPipeline with specific pair error"
        "is not a subtype of step \"second\""
        (checkPipeline reg illTypedFn)

    , expectRight "dependent-output pipeline (all alternatives OK) type-checks"
        (checkPipeline reg dependentOKFn)

    , expectLeftContaining
        "dependent-output pipeline with bad alternative fails at load time"
        "is not a subtype of step \"consume\""
        (checkPipeline reg dependentBadFn)

      -- Runtime: successful single-step and two-step
    , expectRight "runPipeline notice on Braindump succeeds"
        (runPipeline reg noticeFn kb braindump)

    , let result = runPipeline reg decomposeFn kb braindump
      in case result of
           Right outs -> expectEqInt "runPipeline decompose returns 2 step outputs" (length outs) 2
           Left e     -> Fail "runPipeline decompose returns 2 step outputs" ("unexpected Left: " ++ e)

      -- Runtime: step 1 fails validation -> pipeline short-circuits
    , expectLeftContaining
        "runPipeline decompose with bad step 1 fails at step 1"
        "step \"suggest\""
        (runPipeline reg decomposeBadStep1 kb braindump)

      -- Runtime: step 2 fails validation (step 1 succeeds first)
    , expectLeftContaining
        "runPipeline decompose with bad step 2 fails at step 2"
        "step \"generate\""
        (runPipeline reg decomposeBadStep2 kb braindump)

      -- Runtime: picker violates its declared alternatives
    , expectLeftContaining
        "picker returning undeclared alternative is caught at runtime"
        "not in declared alternatives"
        (runPipeline reg pickerLyingFn kb braindump)

      -- Contract check: first step's input enforced against target
    , let fnReqTask = FunctionDef
            (FunctionName "needs-task")
            [ Step
                { stepName   = "work"
                , stepInput  = TypeName "task"
                , stepOutput = StaticOutput (TypeName "text")
                , stepLLM    = \_ _ -> mkValue [("text", VString "ok")]
                }
            ]
      in expectLeftContaining
           "function requiring task rejects a non-task target"
           "not satisfied by target conformance"
           (runPipeline reg fnReqTask kb braindump)

      -- The compositional sanity check: runPipeline on decomposeFn
      -- exposes the inter-step data flow. Step 2's output title
      -- should match what step 1 wrote.
    , case runPipeline reg decomposeFn kb braindump of
        Right outs ->
          let [(_, v1), (_, v2)] = outs
              t1 = case getField (FieldName "title") v1 of
                     VString s -> s
                     _         -> ""
              t2 = case getField (FieldName "title") v2 of
                     VString s -> s
                     _         -> ""
          in if t1 == t2 && t1 /= ""
             then Pass "step 2 reads step 1's output (title is propagated)"
             else Fail "step 2 reads step 1's output (title is propagated)"
                    ("t1=" ++ show t1 ++ " t2=" ++ show t2)
        Left e -> Fail "step 2 reads step 1's output (title is propagated)"
                    ("unexpected Left: " ++ e)

      -- Subsumption smoke checks the pipeline check relies on
    , let ok = isSubtype reg (TypeName "dimension") (TypeName "suggestion")
      in if ok then Pass "dimension <: suggestion"
               else Fail "dimension <: suggestion" "expected True"

    , let ok = isSubtype reg (TypeName "dimension-child") (TypeName "create")
      in if ok then Pass "dimension-child <: create"
               else Fail "dimension-child <: create" "expected True"

    , let bad = isSubtype reg (TypeName "text") (TypeName "dimension")
      in if not bad then Pass "text </: dimension"
                    else Fail "text </: dimension" "expected False"
    ]

main :: IO ()
main = do
  let rs = tests
  mapM_ (putStrLn . showTest) rs
  let failed = [r | r@(Fail _ _) <- rs]
  putStrLn ""
  if null failed
    then do
      putStrLn $ "All " ++ show (length rs) ++ " tests passed."
      exitSuccess
    else do
      putStrLn $ show (length failed) ++ " of " ++ show (length rs) ++ " tests failed."
      exitFailure
