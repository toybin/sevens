-- ConformanceIndex.hs
--
-- Sketch of the conformance index — the cache layer that makes
-- forward, reverse, and ambiguity queries over named types tractable.
--
-- Run: ~/.ghcup/bin/runghc-9.6 docs/sketch/ConformanceIndex.hs
--
-- Scope: levels 1+2 only (intrinsic refinements). No contextual
-- refinements, no cross-node dependencies. Every type is a named,
-- closed definition whose conformance depends ONLY on predicates
-- attached to the node itself.
--
-- The load-bearing property, checked by tests at the bottom:
--
--   For every sequence of (upsertNode | deleteNode | reloadType)
--   operations applied to an initially-empty cache, the resulting
--   incrementally-maintained cache EQUALS the cache obtained by
--   recomputing from scratch against the final graph + registry.
--
-- If that invariant holds in Haskell, we know the incremental update
-- rules are sound before we translate them to SQL triggers and Go.

module Main where

import Data.Map.Strict (Map)
import qualified Data.Map.Strict as Map
import Data.Set (Set)
import qualified Data.Set as Set
import Data.List (foldl', sort, nub)
import System.Exit (exitFailure, exitSuccess)

--------------------------------------------------------------------------
-- Core vocabulary
--------------------------------------------------------------------------

newtype NodeId    = NodeId    String deriving (Eq, Ord, Show)
newtype TypeName  = TypeName  String deriving (Eq, Ord, Show)
newtype Predicate = Predicate String deriving (Eq, Ord, Show)

-- A node is a bag of predicates. For levels 1+2, predicate presence is
-- all that matters; we ignore values to keep the sketch tight. The
-- real kernel will carry values, but the conformance rules below don't
-- change shape.
type Node = Set Predicate

-- The graph is the NodeId -> Node map.
type Graph = Map NodeId Node

--------------------------------------------------------------------------
-- Types (levels 1+2 only)
--
-- Every type declares required and optional predicates and extends
-- exactly one parent (another named type, terminating at a primitive
-- root). No contextual refinements. Refinements at this level reduce
-- to "these predicates must be present" which is the `required` set.
--------------------------------------------------------------------------

data TypeDef = TypeDef
  { tdName     :: TypeName
  , tdParent   :: Maybe TypeName   -- Nothing only for primitives
  , tdRequired :: Set Predicate
  , tdOptional :: Set Predicate
  } deriving (Eq, Show)

type Registry = Map TypeName TypeDef

-- Walk the :extends chain, collecting the union of all required
-- predicates. Levels 1+2 only, so this is a plain union; no refinement
-- override semantics.
composedRequired :: Registry -> TypeName -> Set Predicate
composedRequired reg name =
  case Map.lookup name reg of
    Nothing -> Set.empty
    Just td ->
      let parentReq = case tdParent td of
                        Nothing -> Set.empty
                        Just p  -> composedRequired reg p
      in parentReq `Set.union` tdRequired td

-- A type earns a cache row iff it discriminates — i.e. it has at least
-- one required predicate somewhere in its chain. Types with empty
-- composed-required are vacuously Full for every node; caching "every
-- node" is not informative, and the invariant that incremental == full
-- recompute is easier to maintain if vacuous types are simply not
-- indexed. Subtype queries (is-urgent-task-a-task?) remain answerable
-- from the registry via `isSubtype`, so we don't lose anything.
isCached :: Registry -> TypeName -> Bool
isCached reg name = not (Set.null (composedRequired reg name))

-- Walk ancestors including self. Used for subtype checks.
ancestors :: Registry -> TypeName -> [TypeName]
ancestors reg name =
  case Map.lookup name reg of
    Nothing -> []
    Just td -> name : case tdParent td of
                        Nothing -> []
                        Just p  -> ancestors reg p

isSubtype :: Registry -> TypeName -> TypeName -> Bool
isSubtype reg sub super = super `elem` ancestors reg sub

--------------------------------------------------------------------------
-- Conformance
--------------------------------------------------------------------------

data Status
  = Full                    -- all required predicates present
  | Partial (Set Predicate) -- some required predicates missing (set = missing)
  | None                    -- nothing present, not even partial
  deriving (Eq, Ord, Show)

-- Compute conformance of one (node, type) pair from scratch.
-- Pure function — no cache, no dependencies beyond the inputs.
conformsTo :: Registry -> Node -> TypeName -> Status
conformsTo reg node tname =
  let required = composedRequired reg tname
      missing  = required `Set.difference` node
      present  = required `Set.intersection` node
  in if Set.null required
     then Full                         -- a type with no requirements is always Full
     else if Set.null missing
     then Full
     else if Set.null present
     then None
     else Partial missing

--------------------------------------------------------------------------
-- The index
--
-- Two maps, derivable from (Registry, Graph). Kept in sync by the
-- incremental update functions below, and verified against a full
-- recompute by the invariant test.
--
-- `icForward`  : NodeId   -> [(TypeName, Status)]
-- `icReverse`  : TypeName -> Set NodeId   (Full conformance only)
--
-- The forward map is the primary cache; the reverse map is an
-- inverted view of the same data restricted to Full. In SQL these
-- would be two indexed tables over `node_conformance`.
--------------------------------------------------------------------------

data Index = Index
  { icForward :: Map NodeId (Map TypeName Status)
  , icReverse :: Map TypeName (Set NodeId)
  } deriving (Eq, Show)

emptyIndex :: Index
emptyIndex = Index Map.empty Map.empty

-- Recompute the index from scratch. This is the oracle the
-- incremental path is checked against. Only cached (discriminating)
-- types get rows, and None statuses are canonicalized to absence —
-- the cache stores only meaningful (Full/Partial) facts.
recompute :: Registry -> Graph -> Index
recompute reg graph =
  let typeNames = filter (isCached reg) (Map.keys reg)
      forward   = Map.mapWithKey
                    (\_ node ->
                       Map.fromList
                         [ (t, s) | t <- typeNames
                                  , let s = conformsTo reg node t
                                  , s /= None ])
                    graph
      reverseMap =
        Map.fromListWith Set.union
          [ (t, Set.singleton nid)
          | (nid, rowMap) <- Map.toList forward
          , (t, Full)     <- Map.toList rowMap ]
  in Index forward reverseMap

-- None is represented by absence. Use this helper when writing a
-- status to a row so that both paths stay canonical.
insertStatus :: TypeName -> Status -> Map TypeName Status -> Map TypeName Status
insertStatus t None = Map.delete t
insertStatus t st   = Map.insert t st

--------------------------------------------------------------------------
-- Incremental update
--
-- Three events drive the index:
--
--   upsertNode  nid node        — a node was added or its predicates changed
--   deleteNode  nid             — a node was removed
--   reloadType  tname           — a type definition changed
--
-- Each takes (Registry, Graph, Index) and returns a new (Graph, Index).
-- The Registry is the post-event registry in all three cases. For
-- reloadType, the rule is: touch only conformance rows for THAT type.
-- For upsertNode, the rule is: touch only rows for types whose
-- `composedMentioned` set intersects the changed predicates.
--
-- The narrower the touched set, the more work we save. The invariant
-- test below asserts we don't save too much.
--------------------------------------------------------------------------

-- | All types whose conformance for a node might plausibly depend on
-- the given set of predicates. For levels 1+2, only the composed
-- required set matters — optional predicates don't affect Full-ness,
-- so we don't need to recompute when only optional predicates change.
-- Restricted to cached types.
typesSensitiveTo :: Registry -> Set Predicate -> [TypeName]
typesSensitiveTo reg preds =
  [ tdName td
  | td <- Map.elems reg
  , isCached reg (tdName td)
  , not (Set.null (composedRequired reg (tdName td) `Set.intersection` preds))
  ]

upsertNode :: Registry -> NodeId -> Node -> Graph -> Index -> (Graph, Index)
upsertNode reg nid newNode graph idx =
  let oldNode    = Map.findWithDefault Set.empty nid graph
      changed    = (newNode `Set.difference` oldNode)
                   `Set.union`
                   (oldNode `Set.difference` newNode)
      affected   = typesSensitiveTo reg changed
      -- Recompute conformance for this node against affected types.
      -- Canonicalize None as absence.
      oldRow     = Map.findWithDefault Map.empty nid (icForward idx)
      newRow     = foldl'
                     (\row t -> insertStatus t (conformsTo reg newNode t) row)
                     oldRow
                     affected
      newForward = Map.insert nid newRow (icForward idx)
      -- Update the reverse map based on transitions per affected type.
      newReverse = foldl'
                     (\rev t ->
                        let before = Map.findWithDefault None t oldRow
                            after  = Map.findWithDefault None t newRow
                        in updateReverse t nid before after rev)
                     (icReverse idx)
                     affected
      newGraph   = Map.insert nid newNode graph
  in (newGraph, Index newForward newReverse)

deleteNode :: NodeId -> Graph -> Index -> (Graph, Index)
deleteNode nid graph idx =
  let oldRow     = Map.findWithDefault Map.empty nid (icForward idx)
      newForward = Map.delete nid (icForward idx)
      -- Any type where this node was Full needs to drop it from reverse.
      -- Prune empty sets so the reverse map stays canonical.
      newReverse = Map.foldrWithKey
                     (\t st rev ->
                        case st of
                          Full -> removeFromReverse t nid rev
                          _    -> rev)
                     (icReverse idx)
                     oldRow
      newGraph   = Map.delete nid graph
  in (newGraph, Index newForward newReverse)

-- Reload a single type definition. Touch only rows for that type.
-- `reg` is the post-reload registry. If the reloaded type is no longer
-- cached (became vacuous), drop its rows from the cache entirely. If
-- it remains cached, recompute its conformance across every node.
reloadType :: Registry -> TypeName -> Graph -> Index -> Index
reloadType reg tname graph idx
  | not (isCached reg tname) =
      -- Drop all forward rows for this type and drop the reverse key.
      let fwd' = Map.map (Map.delete tname) (icForward idx)
          rev' = Map.delete tname (icReverse idx)
      in Index fwd' rev'
  | otherwise =
      let recomputeRow nid node (fwd, rev) =
            let oldSt = Map.findWithDefault None tname
                          (Map.findWithDefault Map.empty nid fwd)
                newSt = conformsTo reg node tname
                row   = Map.findWithDefault Map.empty nid fwd
                fwd'  = Map.insert nid (insertStatus tname newSt row) fwd
                rev'  = updateReverse tname nid oldSt newSt rev
            in (fwd', rev')
          (finalFwd, finalRev) =
            Map.foldrWithKey recomputeRow
              (icForward idx, icReverse idx)
              graph
      in Index finalFwd finalRev

-- Helper: promote/demote a (type, node) pair in the reverse map when
-- its status changes. Canonical form: never leave empty sets behind.
updateReverse
  :: TypeName -> NodeId -> Status -> Status
  -> Map TypeName (Set NodeId)
  -> Map TypeName (Set NodeId)
updateReverse t nid before after rev =
  case (before, after) of
    (Full, Full) -> rev
    (Full, _)    -> removeFromReverse t nid rev
    (_,    Full) -> Map.insertWith Set.union t (Set.singleton nid) rev
    _            -> rev

removeFromReverse
  :: TypeName -> NodeId
  -> Map TypeName (Set NodeId) -> Map TypeName (Set NodeId)
removeFromReverse t nid =
  Map.update
    (\s -> let s' = Set.delete nid s
           in if Set.null s' then Nothing else Just s')
    t

--------------------------------------------------------------------------
-- Example registry + graph
--------------------------------------------------------------------------

pCreate, pEdit :: TypeDef
pCreate = TypeDef (TypeName "create") Nothing Set.empty Set.empty
pEdit   = TypeDef (TypeName "edit")   Nothing Set.empty Set.empty

tTask, tNote, tUrgent :: TypeDef
tTask = TypeDef
  { tdName     = TypeName "task"
  , tdParent   = Just (TypeName "create")
  , tdRequired = Set.fromList [Predicate "status", Predicate "deadline"]
  , tdOptional = Set.fromList [Predicate "priority", Predicate "assignee"]
  }

tNote = TypeDef
  { tdName     = TypeName "note"
  , tdParent   = Just (TypeName "create")
  , tdRequired = Set.empty
  , tdOptional = Set.fromList [Predicate "tags"]
  }

tUrgent = TypeDef
  { tdName     = TypeName "urgent-task"
  , tdParent   = Just (TypeName "task")
  , tdRequired = Set.fromList [Predicate "priority"]
  , tdOptional = Set.empty
  }

exampleRegistry :: Registry
exampleRegistry = Map.fromList
  [ (tdName pCreate,  pCreate)
  , (tdName pEdit,    pEdit)
  , (tdName tTask,    tTask)
  , (tdName tNote,    tNote)
  , (tdName tUrgent,  tUrgent)
  ]

mkNode :: [String] -> Node
mkNode = Set.fromList . map Predicate

exampleGraph :: Graph
exampleGraph = Map.fromList
  [ (NodeId "n1", mkNode ["status", "deadline"])                    -- task
  , (NodeId "n2", mkNode ["status", "deadline", "priority"])        -- urgent-task
  , (NodeId "n3", mkNode ["tags"])                                  -- note
  , (NodeId "n4", mkNode [])                                        -- create only
  , (NodeId "n5", mkNode ["status"])                                -- partial task
  ]

--------------------------------------------------------------------------
-- Property check: incremental == recompute
--
-- No QuickCheck dependency. We generate a small fixed battery of event
-- sequences and run both paths, comparing the results.
--------------------------------------------------------------------------

data Event
  = Upsert NodeId Node
  | Delete NodeId
  | ReloadType TypeName
  deriving Show

-- Apply a list of events incrementally and recompute from scratch in
-- parallel. Return (incrementalIndex, recomputedIndex) from the final
-- state. The event sequence may reference types whose definitions
-- are swapped mid-sequence via altRegistries; we keep the registry
-- fixed in this sketch for clarity. (Reload events re-run against the
-- same registry, which still exercises the code path.)
runEventsBoth :: Registry -> [Event] -> (Index, Index)
runEventsBoth reg events =
  let step (g, i) ev = case ev of
        Upsert nid node     -> upsertNode reg nid node g i
        Delete nid          -> deleteNode nid g i
        ReloadType t        -> (g, reloadType reg t g i)
      (finalGraph, finalIdx) = foldl' step (Map.empty, emptyIndex) events
      recomputed             = recompute reg finalGraph
  in (finalIdx, recomputed)

eventBatteries :: [(String, [Event])]
eventBatteries =
  [ ( "empty"
    , [] )
  , ( "insert one task"
    , [ Upsert (NodeId "a") (mkNode ["status", "deadline"]) ] )
  , ( "insert, then add priority"
    , [ Upsert (NodeId "a") (mkNode ["status", "deadline"])
      , Upsert (NodeId "a") (mkNode ["status", "deadline", "priority"])
      ] )
  , ( "insert urgent-task, drop priority"
    , [ Upsert (NodeId "a") (mkNode ["status", "deadline", "priority"])
      , Upsert (NodeId "a") (mkNode ["status", "deadline"])
      ] )
  , ( "insert and delete"
    , [ Upsert (NodeId "a") (mkNode ["status", "deadline"])
      , Delete (NodeId "a")
      ] )
  , ( "multiple nodes"
    , [ Upsert (NodeId "a") (mkNode ["status", "deadline"])
      , Upsert (NodeId "b") (mkNode ["tags"])
      , Upsert (NodeId "c") (mkNode ["status"])       -- partial task
      , Upsert (NodeId "d") (mkNode ["status", "deadline", "priority"])
      ] )
  , ( "delete mid-stream and re-add"
    , [ Upsert (NodeId "a") (mkNode ["status", "deadline"])
      , Upsert (NodeId "b") (mkNode ["status", "deadline", "priority"])
      , Delete (NodeId "a")
      , Upsert (NodeId "a") (mkNode ["tags"])
      ] )
  , ( "reload type with no graph change"
    , [ Upsert (NodeId "a") (mkNode ["status", "deadline"])
      , Upsert (NodeId "b") (mkNode ["status", "deadline", "priority"])
      , ReloadType (TypeName "task")
      , ReloadType (TypeName "urgent-task")
      ] )
  , ( "churn"
    , [ Upsert (NodeId "a") (mkNode ["status"])
      , Upsert (NodeId "a") (mkNode ["status", "deadline"])
      , Upsert (NodeId "a") (mkNode ["status", "deadline", "priority"])
      , Upsert (NodeId "a") (mkNode ["status", "deadline"])
      , Upsert (NodeId "a") (mkNode [])
      , Upsert (NodeId "a") (mkNode ["status", "deadline", "priority"])
      , Delete (NodeId "a")
      , Upsert (NodeId "a") (mkNode ["tags"])
      ] )
  ]

--------------------------------------------------------------------------
-- Test runner
--------------------------------------------------------------------------

data TestResult = Pass String | Fail String String

showTest :: TestResult -> String
showTest (Pass name)        = "PASS  " ++ name
showTest (Fail name reason) = "FAIL  " ++ name ++ "\n        " ++ reason

expectEq :: (Eq a, Show a) => String -> a -> a -> TestResult
expectEq name actual expected
  | actual == expected = Pass name
  | otherwise = Fail name ("expected " ++ show expected ++ "\n        got " ++ show actual)

-- Run every battery, compare incremental vs recomputed.
invariantTests :: [TestResult]
invariantTests =
  [ let (inc, recomp) = runEventsBoth exampleRegistry events
    in expectEq ("invariant: " ++ name) inc recomp
  | (name, events) <- eventBatteries
  ]

-- Spot checks on conformance and lookups against a known graph.
-- Only types with at least one required predicate are cached, so
-- `create`, `edit`, and `note` don't produce cache rows — subtype
-- questions about them are answered via `isSubtype` on the registry.
spotTests :: [TestResult]
spotTests =
  let reg = exampleRegistry
      idx = recompute reg exampleGraph
      forward nid = Map.findWithDefault Map.empty nid (icForward idx)
      statusOf nid t = Map.findWithDefault None t (forward nid)
      reverseOf t = Map.findWithDefault Set.empty t (icReverse idx)
  in [ expectEq "n1 is Full task"
         (statusOf (NodeId "n1") (TypeName "task")) Full
     , expectEq "n1 is Partial urgent-task (missing priority)"
         (statusOf (NodeId "n1") (TypeName "urgent-task"))
         (Partial (Set.singleton (Predicate "priority")))
     , expectEq "n2 is Full urgent-task"
         (statusOf (NodeId "n2") (TypeName "urgent-task")) Full
     , expectEq "n2 is Full task (inherited)"
         (statusOf (NodeId "n2") (TypeName "task")) Full
     , expectEq "n5 is Partial task (missing deadline)"
         (statusOf (NodeId "n5") (TypeName "task"))
         (Partial (Set.singleton (Predicate "deadline")))
     , expectEq "reverse: tasks are {n1, n2}"
         (reverseOf (TypeName "task"))
         (Set.fromList [NodeId "n1", NodeId "n2"])
     , expectEq "reverse: urgent-tasks are {n2}"
         (reverseOf (TypeName "urgent-task"))
         (Set.fromList [NodeId "n2"])
     , expectEq "note not in forward cache (no required predicates)"
         (Map.member (TypeName "note") (forward (NodeId "n3"))) False
     , expectEq "note not in reverse cache"
         (Map.member (TypeName "note") (icReverse idx)) False
     , expectEq "subtype: urgent-task <: task"
         (isSubtype reg (TypeName "urgent-task") (TypeName "task")) True
     , expectEq "subtype: task <: create"
         (isSubtype reg (TypeName "task") (TypeName "create")) True
     , expectEq "subtype: note </: task"
         (isSubtype reg (TypeName "note") (TypeName "task")) False
     ]

main :: IO ()
main = do
  let rs = invariantTests ++ spotTests
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
