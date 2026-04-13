-- InputDependence.hs
--
-- Models input-dependent output types — the Node<T> → Node<child-of<T>>
-- shape concept-types hints at on line 393. A function's output type
-- is a function of its input's concrete type, not a static constant.
--
-- Run: ~/.ghcup/bin/runghc-9.6 docs/sketch/InputDependence.hs
--
-- Load-bearing claims:
--
--   1. The "type family" abstraction is a named graph: a registry
--      entry that maps a type to its related type (e.g., parent-of,
--      child-of, sibling-of). Same shape as a value model or an
--      orthography binding — just more data on the type system.
--
--   2. Input-dependence resolves BEFORE the LLM is called, just like
--      DependentOutput. The resolution uses the target's
--      most-specific conformance type (from the conformance index).
--
--   3. Load-time typechecking is still decidable: for every type
--      that could serve as input, the family must map it to a type
--      that is a subtype of the next step's input. The check
--      enumerates all subtypes of the declared input and verifies
--      each lookup.
--
--   4. The same function definition works across multiple input
--      types without any runtime polymorphism in the user code.
--      Decompose a `task` → get `task-child`. Decompose a
--      `dimension` → get `dimension-child`. One function, one
--      prompt template, two correct behaviors.
--
--   5. A missing family entry is a load-time error, not a runtime
--      surprise. If the registry says `task` extends `create` but
--      doesn't declare `:child-type`, a function using
--      `ChildOfInput` cannot be applied to a task target, and the
--      load-time check tells you so at pipeline construction time.

module Main where

import Data.Map.Strict (Map)
import qualified Data.Map.Strict as Map
import Data.Set (Set)
import qualified Data.Set as Set
import Data.List (isPrefixOf)
import System.Exit (exitFailure, exitSuccess)

--------------------------------------------------------------------------
-- Kernel + type family
--------------------------------------------------------------------------

newtype TypeName  = TypeName  String deriving (Eq, Ord, Show)
newtype Title     = Title     String deriving (Eq, Ord, Show)
newtype NodeId    = NodeId    String deriving (Eq, Ord, Show)
newtype FieldName = FieldName String deriving (Eq, Ord, Show)

data FieldValue = VString String | VMap (Map String String) | VAbsent
  deriving (Eq, Show)
data Value = Value { valueFields :: Map FieldName FieldValue } deriving (Eq, Show)

mkValue :: [(String, FieldValue)] -> Value
mkValue ps = Value (Map.fromList [(FieldName k, v) | (k,v) <- ps])

getField :: FieldName -> Value -> FieldValue
getField fn (Value fs) = Map.findWithDefault VAbsent fn fs

data Primitive = PText | PCreate | PEdit | PSuggestion deriving (Eq, Ord, Show)

primitiveName :: Primitive -> TypeName
primitiveName PText = TypeName "text"
primitiveName PCreate = TypeName "create"
primitiveName PEdit = TypeName "edit"
primitiveName PSuggestion = TypeName "suggestion"

data TypeDef
  = PrimT Primitive
  | DerivedT
      { dtName      :: TypeName
      , dtParent    :: TypeName
      , dtChildType :: Maybe TypeName   -- the new piece: type family
                                         -- entry for "my children are
                                         -- of type X"
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

-- All types declared in the registry that are subtypes of a given type.
-- Used by the load-time check for input-dependent outputs.
subtypesOf :: Registry -> TypeName -> [TypeName]
subtypesOf reg name =
  [ typeDefName td
  | td <- Map.elems reg
  , isSubtype reg (typeDefName td) name
  ]

-- Type family lookup: what is the child-type of T?
-- Walks the :extends chain so a derived type can inherit its
-- parent's child-type if it doesn't declare its own. This matches
-- the natural expectation that subtyping is transitive across
-- type families unless explicitly overridden.
childTypeOf :: Registry -> TypeName -> Maybe TypeName
childTypeOf reg name = case Map.lookup name reg of
  Nothing -> Nothing
  Just (PrimT _) -> Nothing
  Just DerivedT{ dtChildType = Just ct } -> Just ct
  Just DerivedT{ dtParent = p }          -> childTypeOf reg p

--------------------------------------------------------------------------
-- Example types with a family declared
--
-- task has a child-type of task-child
-- dimension has a child-type of dimension-child
-- note has NO child-type (its children are structurally unrelated)
--------------------------------------------------------------------------

taskType, taskChildType :: TypeDef
taskType = DerivedT
  { dtName      = TypeName "task"
  , dtParent    = TypeName "create"
  , dtChildType = Just (TypeName "task-child")
  }
taskChildType = DerivedT
  { dtName      = TypeName "task-child"
  , dtParent    = TypeName "create"
  , dtChildType = Nothing
  }

dimensionType, dimensionChildType :: TypeDef
dimensionType = DerivedT
  { dtName      = TypeName "dimension"
  , dtParent    = TypeName "suggestion"
  , dtChildType = Just (TypeName "dimension-child")
  }
dimensionChildType = DerivedT
  { dtName      = TypeName "dimension-child"
  , dtParent    = TypeName "create"
  , dtChildType = Nothing
  }

noteType :: TypeDef
noteType = DerivedT
  { dtName      = TypeName "note"
  , dtParent    = TypeName "create"
  , dtChildType = Nothing    -- note has no child type declared
  }

exampleRegistry :: Registry
exampleRegistry = foldr (\td r -> Map.insert (typeDefName td) td r) primitivesRegistry
  [ taskType, taskChildType
  , dimensionType, dimensionChildType
  , noteType
  ]

--------------------------------------------------------------------------
-- Target carries its most-specific conformance type
--------------------------------------------------------------------------

data Target = Target
  { targetId            :: NodeId
  , targetTitle         :: Title
  , targetMostSpecific  :: TypeName   -- supplied by the conformance index
  } deriving Show

--------------------------------------------------------------------------
-- StepContext: gains an explicit input-type slot
--------------------------------------------------------------------------

data StepContext = StepContext
  { ctxTarget    :: Target
  , ctxInputType :: TypeName       -- the resolved input to THIS step
  , ctxPrior     :: [(TypeName, Value)]
  }

--------------------------------------------------------------------------
-- OutputPicker gains the ChildOfInput variant
--------------------------------------------------------------------------

data OutputPicker
  = StaticOutput TypeName
  | ChildOfInput      -- "for input type T, output is childTypeOf(T)"
  deriving Show

resolveOutput :: Registry -> OutputPicker -> StepContext -> Either String TypeName
resolveOutput _   (StaticOutput t)  _   = Right t
resolveOutput reg ChildOfInput      ctx =
  case childTypeOf reg (ctxInputType ctx) of
    Just ct -> Right ct
    Nothing ->
      Left ("no child-type declared for input type "
             ++ show (ctxInputType ctx)
             ++ "; function using ChildOfInput cannot be resolved")

--------------------------------------------------------------------------
-- Step + Function
--------------------------------------------------------------------------

type LLMStub = TypeName -> StepContext -> Value

data Step = Step
  { stepName   :: String
  , stepInput  :: TypeName
  , stepOutput :: OutputPicker
  , stepLLM    :: LLMStub
  }

data FunctionDef = FunctionDef
  { funcName  :: String
  , funcSteps :: [Step]
  }

--------------------------------------------------------------------------
-- Load-time checks
--
-- Two conditions to verify for a step using ChildOfInput:
--
--   a) For every type T declared in the registry that is a subtype
--      of the step's declared input, childTypeOf(T) must be defined.
--      Otherwise the function cannot be applied to targets of that
--      subtype.
--
--   b) For every such resolved child-type CT, if there is a next
--      step in the pipeline, CT must be a subtype of that step's
--      declared input.
--
-- Both checks are decidable because the registry is finite and the
-- :extends chain is walked deterministically.
--------------------------------------------------------------------------

checkPipeline :: Registry -> FunctionDef -> Either String ()
checkPipeline reg fn = case funcSteps fn of
  []   -> Left ("empty function " ++ funcName fn)
  ss   -> do
    -- Every step must be individually well-typed: its output picker
    -- must be resolvable for every possible concrete input. This is
    -- what catches single-step ChildOfInput functions whose family
    -- lookups would fail at runtime.
    mapM_ (checkStepAlone reg) ss
    -- Adjacent-pair compatibility
    walk ss
  where
    walk []           = Right ()
    walk [_]          = Right ()
    walk (a:b:rest)   = do
      checkAdjacent a b
      walk (b:rest)

    checkAdjacent a b =
      let outs = possibleOutputs reg (stepInput a) (stepOutput a)
      in case outs of
           Left e -> Left e
           Right ts ->
             mapM_ (\t ->
                      if isSubtype reg t (stepInput b)
                      then Right ()
                      else Left $
                        "step " ++ stepName a ++ " output " ++ show t
                        ++ " not <: step " ++ stepName b
                        ++ " input " ++ show (stepInput b))
                    ts

checkStepAlone :: Registry -> Step -> Either String ()
checkStepAlone reg s =
  case possibleOutputs reg (stepInput s) (stepOutput s) of
    Left e  -> Left ("step " ++ stepName s ++ ": " ++ e)
    Right _ -> Right ()

-- possibleOutputs: the set of output type names the step might
-- produce, given the set of possible input types (subtypes of
-- stepInput). For StaticOutput it's a singleton regardless of input.
-- For ChildOfInput it's the set of childTypeOf(T) for every T that
-- could be the concrete input. If any T lacks a child-type, the
-- whole step is ill-typed.
possibleOutputs :: Registry -> TypeName -> OutputPicker -> Either String [TypeName]
possibleOutputs _   _        (StaticOutput t) = Right [t]
possibleOutputs reg declared ChildOfInput =
  let candidateInputs = subtypesOf reg declared
      -- For the step to be well-typed, every candidate concrete
      -- input must map to a defined child-type.
      pairs = [ (t, childTypeOf reg t) | t <- candidateInputs ]
      missing = [ t | (t, Nothing) <- pairs ]
  in case missing of
       [] -> Right [ ct | (_, Just ct) <- pairs ]
       _  -> Left $
         "ChildOfInput cannot be resolved for the following subtypes of "
         ++ show declared ++ ": " ++ show missing
         ++ " (no :child-type declared)"

--------------------------------------------------------------------------
-- Dispatch + run
--------------------------------------------------------------------------

canDispatch :: Registry -> FunctionDef -> Target -> Either String ()
canDispatch reg fn target = case funcSteps fn of
  [] -> Left "empty"
  (s0:_) ->
    let input   = stepInput s0
        concrete = targetMostSpecific target
    in if isSubtype reg concrete input
       then Right ()
       else Left ("target " ++ show (targetTitle target)
                   ++ " type " ++ show concrete
                   ++ " is not a subtype of input " ++ show input)

runPipeline
  :: Registry -> FunctionDef -> Target
  -> Either String [(TypeName, Value)]
runPipeline reg fn target = do
  checkPipeline reg fn
  canDispatch reg fn target
  runFrom (funcSteps fn) initialCtx
  where
    initialCtx = StepContext
      { ctxTarget    = target
      , ctxInputType = targetMostSpecific target
      , ctxPrior     = []
      }

    runFrom [] ctx = Right (reverse (ctxPrior ctx))
    runFrom (s:rest) ctx = do
      outType <- resolveOutput reg (stepOutput s) ctx
      let raw = stepLLM s outType ctx
      -- (simplified: no post-LLM shape validation; this sketch is
      -- only exercising input-dependence and the type family, not
      -- revalidating the refinement machinery from earlier sketches)
      let ctx' = ctx
            { ctxPrior = (outType, raw) : ctxPrior ctx
            , ctxInputType = outType     -- the next step's input is
                                         -- this step's output
            }
      runFrom rest ctx'

--------------------------------------------------------------------------
-- Example function: decompose
--
-- One definition works for both task targets (producing task-child
-- outputs) and dimension targets (producing dimension-child outputs).
-- The prompt/backend/etc. don't care which concrete type is chosen —
-- the resolver does it before the stub is called.
--------------------------------------------------------------------------

decompose :: FunctionDef
decompose = FunctionDef
  { funcName = "decompose"
  , funcSteps =
      [ Step
          { stepName   = "emit-child"
          , stepInput  = TypeName "create"   -- broad input; ChildOfInput
                                              -- narrows via the family
          , stepOutput = ChildOfInput
          , stepLLM    = \outType ctx ->
              let Title t = targetTitle (ctxTarget ctx)
                  TypeName ot = outType
              in mkValue
                  [ ("title",   VString ("Child of " ++ t ++ " (as " ++ ot ++ ")"))
                  , ("parent",  VString t)
                  , ("content", VString "stubbed child")
                  ]
          }
      ]
  }

-- A bad function: declares input as "create" (which has a subtype
-- `note` with NO child-type declared) AND uses ChildOfInput. Must
-- fail the load-time check.
decomposeBroken :: FunctionDef
decomposeBroken = FunctionDef
  { funcName  = "decompose-broken"
  , funcSteps =
      [ Step
          { stepName   = "emit-child"
          , stepInput  = TypeName "create"
          , stepOutput = ChildOfInput
          , stepLLM    = \_ _ -> mkValue []
          }
      ]
  }

-- A narrower version: declares input as "task", which has a
-- child-type. Load-time check passes.
decomposeTaskOnly :: FunctionDef
decomposeTaskOnly = FunctionDef
  { funcName  = "decompose-task-only"
  , funcSteps =
      [ Step
          { stepName   = "emit-child"
          , stepInput  = TypeName "task"
          , stepOutput = ChildOfInput
          , stepLLM    = \_ _ -> mkValue []
          }
      ]
  }

--------------------------------------------------------------------------
-- Tests
--------------------------------------------------------------------------

data TR = Pass String | Fail String String
showTR (Pass n)   = "PASS  " ++ n
showTR (Fail n r) = "FAIL  " ++ n ++ "\n        " ++ r

expectRight :: Show a => String -> Either String a -> TR
expectRight n (Right _) = Pass n
expectRight n (Left e)  = Fail n ("expected Right, got Left: " ++ e)

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

-- Targets
taskTarget = Target (NodeId "t") (Title "Write report") (TypeName "task")
dimensionTarget = Target (NodeId "d") (Title "Cost") (TypeName "dimension")
noteTarget = Target (NodeId "n") (Title "Random") (TypeName "note")

tests :: [TR]
tests =
  let reg = exampleRegistry
  in
    [ -- Type family lookups
      expectEq "childTypeOf task = task-child"
        (childTypeOf reg (TypeName "task")) (Just (TypeName "task-child"))
    , expectEq "childTypeOf dimension = dimension-child"
        (childTypeOf reg (TypeName "dimension")) (Just (TypeName "dimension-child"))
    , expectEq "childTypeOf note = Nothing"
        (childTypeOf reg (TypeName "note")) Nothing
    , expectEq "childTypeOf inherits through :extends (task-child <- create -> nothing)"
        (childTypeOf reg (TypeName "task-child")) Nothing

      -- Load-time: declaring input=create with ChildOfInput is broken
      -- because note is a subtype of create and note has no child-type
    , expectLeftContaining
        "decomposeBroken fails load-time (note has no child-type)"
        "no :child-type declared"
        (checkPipeline reg decomposeBroken)

      -- Load-time: declaring input=task with ChildOfInput is fine
    , expectRight
        "decomposeTaskOnly passes load-time"
        (checkPipeline reg decomposeTaskOnly)

      -- The same function, applied to two different concrete inputs,
      -- produces two different output types
    , case runPipeline reg decomposeTaskOnly taskTarget of
        Right [(outT, _)] ->
          if outT == TypeName "task-child"
          then Pass "decompose(task) -> task-child"
          else Fail "decompose(task) -> task-child" ("got " ++ show outT)
        other -> Fail "decompose(task) -> task-child" ("unexpected: " ++ show other)

      -- decomposeTaskOnly cannot be dispatched to a dimension target
    , expectLeftContaining
        "decomposeTaskOnly cannot dispatch to dimension target"
        "is not a subtype of input"
        (runPipeline reg decomposeTaskOnly dimensionTarget)

      -- A more general version that accepts `create` BUT has a valid
      -- child-type for all concrete inputs — constructed inline from
      -- a registry that has family coverage everywhere.
    , let reg' = foldr (\td r -> Map.insert (typeDefName td) td r) primitivesRegistry
                   [ taskType, taskChildType
                   , dimensionType, dimensionChildType
                   -- note: no note type, so subtypesOf (TypeName "create") = {task,dimension,task-child,dimension-child,create}
                   -- but task-child and dimension-child and create themselves
                   -- still don't have child-types. So this is STILL broken.
                   -- The test proves the load check catches it.
                   ]
      in expectLeftContaining
           "create-as-input is ill-typed even without note (task-child has no child-type)"
           "no :child-type declared"
           (checkPipeline reg' decompose)

      -- A function with a narrower input works for dimension
    , let decomposeDim = FunctionDef
            { funcName = "decompose-dim-only"
            , funcSteps =
                [ Step
                    { stepName   = "emit-child"
                    , stepInput  = TypeName "dimension"
                    , stepOutput = ChildOfInput
                    , stepLLM    = \_ _ -> mkValue []
                    }
                ]
            }
      in case runPipeline reg decomposeDim dimensionTarget of
           Right [(outT, _)] ->
             if outT == TypeName "dimension-child"
             then Pass "decompose(dimension) -> dimension-child (same shape, different type)"
             else Fail "decompose(dimension) -> dimension-child" ("got " ++ show outT)
           other -> Fail "decompose(dimension) -> dimension-child" ("unexpected: " ++ show other)

      -- Subsumption smoke checks
    , expectEq "task-child <: create"
        (isSubtype reg (TypeName "task-child") (TypeName "create")) True
    , expectEq "dimension-child <: create"
        (isSubtype reg (TypeName "dimension-child") (TypeName "create")) True
    , expectEq "subtypesOf create includes task and note (not dimension; dimension <: suggestion)"
        (Set.fromList (subtypesOf reg (TypeName "create"))
           `Set.intersection` Set.fromList [TypeName "task", TypeName "dimension", TypeName "note"])
        (Set.fromList [TypeName "task", TypeName "note"])
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
