-- TypesKernel.hs
--
-- Sketch of the sevens type kernel in Haskell. Models primitives,
-- derived types, dependent refinements, and dependent shapes.
--
-- Run: ~/.ghcup/bin/runghc-9.6 docs/sketch/TypesKernel.hs
--
-- The kernel treats types as RUNTIME DATA, not as Haskell types. GHC
-- is only used to check that the data structures and functions hold
-- together. A sevens type registry is a Map TypeName TypeDef; values
-- (what the LLM returns) are field maps; validation is a pure function
-- over (Registry, KB, TypeName, Value).

module Main where

import Data.Map.Strict (Map)
import qualified Data.Map.Strict as Map
import Data.List (isSuffixOf, isPrefixOf)
import System.Exit (exitFailure, exitSuccess)

--------------------------------------------------------------------------
-- Names
--------------------------------------------------------------------------

newtype TypeName  = TypeName  String deriving (Eq, Ord, Show)
newtype FieldName = FieldName String deriving (Eq, Ord, Show)
newtype Title     = Title     String deriving (Eq, Ord, Show)

--------------------------------------------------------------------------
-- Primitives
--
-- The four built-in types. Everything else extends one of these. The
-- JSON shape of a primitive is declared here in the kernel and nowhere
-- else. No shape information lives in function prompts or Go code.
--------------------------------------------------------------------------

data Primitive = PText | PCreate | PEdit | PSuggestion
  deriving (Eq, Ord, Show)

primitiveName :: Primitive -> TypeName
primitiveName PText       = TypeName "text"
primitiveName PCreate     = TypeName "create"
primitiveName PEdit       = TypeName "edit"
primitiveName PSuggestion = TypeName "suggestion"

--------------------------------------------------------------------------
-- Shapes
--------------------------------------------------------------------------

data FieldKind
  = FKString     -- arbitrary string
  | FKContent    -- markdown body
  | FKExtra      -- frontmatter map (string -> string)
  deriving (Eq, Show)

data FieldSpec = FieldSpec
  { fsName     :: FieldName
  , fsKind     :: FieldKind
  , fsRequired :: Bool
  } deriving Show

primitiveShape :: Primitive -> [FieldSpec]
primitiveShape PText =
  [ FieldSpec (FieldName "text") FKString True
  ]
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

--------------------------------------------------------------------------
-- Values (what the LLM returns, after JSON decoding)
--------------------------------------------------------------------------

data FieldValue
  = VString String
  | VMap (Map String String)
  | VAbsent
  deriving (Eq, Show)

data Value = Value { fields :: Map FieldName FieldValue }
  deriving (Eq, Show)

mkValue :: [(String, FieldValue)] -> Value
mkValue pairs = Value (Map.fromList [(FieldName k, v) | (k, v) <- pairs])

getField :: FieldName -> Value -> FieldValue
getField fn (Value fs) = Map.findWithDefault VAbsent fn fs

--------------------------------------------------------------------------
-- KB (the minimum needed to exercise dependent refinements)
--------------------------------------------------------------------------

data KB = KB { nodes :: Map Title String } deriving Show

resolve :: KB -> Title -> Maybe String
resolve (KB ns) t = Map.lookup t ns

emptyKB :: KB
emptyKB = KB Map.empty

--------------------------------------------------------------------------
-- Refinements
--
-- A refinement is a named predicate on a value. Intrinsic refinements
-- are pure (levels 1 and 2 in the design doc). Contextual refinements
-- take the KB as an extra argument (level 3: dependent refinements).
--------------------------------------------------------------------------

data Refinement
  = Intrinsic  (Value -> Either String ())
  | Contextual (KB -> Value -> Either String ())

data NamedRefinement = NamedRefinement
  { refName  :: String
  , refCheck :: Refinement
  }

--------------------------------------------------------------------------
-- Type definitions
--------------------------------------------------------------------------

data TypeDef
  = PrimT Primitive
  | DerivedT
      { dtName        :: TypeName
      , dtParent      :: TypeName       -- must resolve in the registry
      , dtExtraFields :: [FieldSpec]    -- added to parent's shape
      , dtRefinements :: [NamedRefinement]
      }

typeDefName :: TypeDef -> TypeName
typeDefName (PrimT p)               = primitiveName p
typeDefName (DerivedT { dtName = n }) = n

--------------------------------------------------------------------------
-- Registry
--------------------------------------------------------------------------

type Registry = Map TypeName TypeDef

insertType :: TypeDef -> Registry -> Registry
insertType td reg = Map.insert (typeDefName td) td reg

mkRegistry :: [TypeDef] -> Registry
mkRegistry = foldr insertType Map.empty

primitivesRegistry :: Registry
primitivesRegistry = mkRegistry
  [ PrimT PText, PrimT PCreate, PrimT PEdit, PrimT PSuggestion ]

--------------------------------------------------------------------------
-- Subsumption
--
-- T' <: T iff T appears in the ancestors of T'. Ancestors walk the
-- :extends chain to its primitive root.
--------------------------------------------------------------------------

ancestors :: Registry -> TypeName -> [TypeName]
ancestors reg name =
  case Map.lookup name reg of
    Nothing        -> []
    Just (PrimT p) -> [primitiveName p]
    Just dt@DerivedT{} -> dtName dt : ancestors reg (dtParent dt)

isSubtype :: Registry -> TypeName -> TypeName -> Bool
isSubtype reg sub super = super `elem` ancestors reg sub

rootPrimitive :: Registry -> TypeName -> Maybe Primitive
rootPrimitive reg name =
  case Map.lookup name reg of
    Nothing        -> Nothing
    Just (PrimT p) -> Just p
    Just dt@DerivedT{} -> rootPrimitive reg (dtParent dt)

--------------------------------------------------------------------------
-- Shape composition
--
-- Walk from primitive root up to the named type, collecting fields.
-- Derived-type fields override parent fields by name (right-biased).
--------------------------------------------------------------------------

composedShape :: Registry -> TypeName -> [FieldSpec]
composedShape reg name =
  case Map.lookup name reg of
    Nothing            -> []
    Just (PrimT p)     -> primitiveShape p
    Just dt@DerivedT{} -> overrideFields (composedShape reg (dtParent dt)) (dtExtraFields dt)

overrideFields :: [FieldSpec] -> [FieldSpec] -> [FieldSpec]
overrideFields old new =
  let newNames = [fsName f | f <- new]
      kept     = filter (\f -> fsName f `notElem` newNames) old
  in kept ++ new

--------------------------------------------------------------------------
-- Refinement collection
--
-- Walk the chain root-first and concatenate all refinements. Ordering
-- matters for error reporting: parent refinements fire before child.
--------------------------------------------------------------------------

collectRefinements :: Registry -> TypeName -> [NamedRefinement]
collectRefinements reg name =
  case Map.lookup name reg of
    Nothing            -> []
    Just (PrimT _)     -> []
    Just dt@DerivedT{} -> collectRefinements reg (dtParent dt) ++ dtRefinements dt

--------------------------------------------------------------------------
-- Schema instruction
--
-- Text composition. This is what the executor injects into the system
-- prompt. There is no other source of shape info — prompt .md files
-- never mention JSON.
--------------------------------------------------------------------------

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
-- Validation
--
-- Parse-time check: shape (required fields present) + all refinements
-- in the chain. Fails loudly with the first broken clause.
--------------------------------------------------------------------------

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
      let fv = getField (fsName f) val
          FieldName fn = fsName f
      in case (fsRequired f, fv) of
           (True,  VAbsent)    -> Left ("field " ++ fn ++ " required but absent")
           (True,  VString "") -> Left ("field " ++ fn ++ " required but empty")
           _                   -> Right ()

runRefinement :: KB -> Value -> NamedRefinement -> Either String ()
runRefinement _  val (NamedRefinement n (Intrinsic  f)) =
  case f val    of Left e -> Left (n ++ ": " ++ e); Right () -> Right ()
runRefinement kb val (NamedRefinement n (Contextual f)) =
  case f kb val of Left e -> Left (n ++ ": " ++ e); Right () -> Right ()

--------------------------------------------------------------------------
-- Example derived types
--
-- task            - level 2 (intra-value refinement; no KB)
-- valid-edit      - level 3 (dependent refinement on file)
-- discussion-turn - level 3 (dependent refinement on last-line suffix)
-- discussion-start- level 1 (intrinsic refinement on title prefix)
--------------------------------------------------------------------------

taskType :: TypeDef
taskType = DerivedT
  { dtName        = TypeName "task"
  , dtParent      = TypeName "create"
  , dtExtraFields = []
  , dtRefinements =
      [ NamedRefinement "status and deadline present in extra" $
          Intrinsic $ \v ->
            case getField (FieldName "extra") v of
              VMap m ->
                let missing = filter (\k -> not (Map.member k m)) ["status", "deadline"]
                in if null missing
                   then Right ()
                   else Left ("missing extra keys: " ++ show missing)
              _ -> Left "extra must be a map"
      ]
  }

validEditType :: TypeDef
validEditType = DerivedT
  { dtName        = TypeName "valid-edit"
  , dtParent      = TypeName "edit"
  , dtExtraFields = []
  , dtRefinements =
      [ NamedRefinement "file must resolve in KB" $
          Contextual $ \kb v ->
            case getField (FieldName "file") v of
              VString f ->
                case resolve kb (Title f) of
                  Just _  -> Right ()
                  Nothing -> Left ("file " ++ show f ++ " does not resolve in KB")
              _ -> Left "file must be a string"
      ]
  }

discussionTurnType :: TypeDef
discussionTurnType = DerivedT
  { dtName        = TypeName "discussion-turn"
  , dtParent      = TypeName "valid-edit"
  , dtExtraFields = []
  , dtRefinements =
      [ NamedRefinement "old_text is a suffix of the last line of file" $
          Contextual $ \kb v ->
            case (getField (FieldName "file") v, getField (FieldName "old_text") v) of
              (VString f, VString ot) ->
                case resolve kb (Title f) of
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

discussionStartType :: TypeDef
discussionStartType = DerivedT
  { dtName        = TypeName "discussion-start"
  , dtParent      = TypeName "create"
  , dtExtraFields = []
  , dtRefinements =
      [ NamedRefinement "title has 'Discussion - ' prefix" $
          Intrinsic $ \v ->
            case getField (FieldName "title") v of
              VString t ->
                if "Discussion - " `isPrefixOf` t
                then Right ()
                else Left ("title must start with 'Discussion - ', got " ++ show t)
              _ -> Left "title must be a string"
      ]
  }

exampleRegistry :: Registry
exampleRegistry = foldr insertType primitivesRegistry
  [ taskType, validEditType, discussionTurnType, discussionStartType ]

--------------------------------------------------------------------------
-- Level 4: dependent shape
--
-- The output type of `discuss` is chosen by a function of KB state
-- and target. This is not a type; it is a TypeName picker that runs
-- before the LLM is called.
--------------------------------------------------------------------------

discussOutputType :: KB -> Title -> TypeName
discussOutputType kb (Title target) =
  case resolve kb (Title ("Discussion - " ++ target)) of
    Nothing -> TypeName "discussion-start"
    Just _  -> TypeName "discussion-turn"

--------------------------------------------------------------------------
-- Tests
--------------------------------------------------------------------------

data TestResult = Pass String | Fail String String

showTest :: TestResult -> String
showTest (Pass name)        = "PASS  " ++ name
showTest (Fail name reason) = "FAIL  " ++ name ++ "\n        " ++ reason

expectEq :: (Eq a, Show a) => String -> a -> a -> TestResult
expectEq name actual expected
  | actual == expected = Pass name
  | otherwise = Fail name ("expected " ++ show expected ++ ", got " ++ show actual)

expectRight :: Show e => String -> Either e a -> TestResult
expectRight name (Right _) = Pass name
expectRight name (Left e)  = Fail name ("expected Right, got Left " ++ show e)

expectLeft :: String -> Either String a -> TestResult
expectLeft name (Left _)  = Pass name
expectLeft name (Right _) = Fail name "expected Left, got Right"

sampleKB :: KB
sampleKB = KB $ Map.fromList
  [ ( Title "Discussion - CI/CD Substrate"
    , "# Discussion\n\n**[agent 2026-04-13]** First question here.\n**[agent 2026-04-13]** The last line is this one."
    )
  , ( Title "Braindump"
    , "# overview\n\nTop-level node."
    )
  ]

tests :: [TestResult]
tests =
  let reg = exampleRegistry
      kb  = sampleKB

      -- Subsumption
      t01 = expectEq "task <: create"
              (isSubtype reg (TypeName "task") (TypeName "create")) True
      t02 = expectEq "create </: task"
              (isSubtype reg (TypeName "create") (TypeName "task")) False
      t03 = expectEq "discussion-turn <: edit"
              (isSubtype reg (TypeName "discussion-turn") (TypeName "edit")) True
      t04 = expectEq "discussion-turn <: valid-edit"
              (isSubtype reg (TypeName "discussion-turn") (TypeName "valid-edit")) True
      t05 = expectEq "task </: edit"
              (isSubtype reg (TypeName "task") (TypeName "edit")) False

      -- Primitive roots
      t06 = expectEq "rootPrimitive task = create"
              (rootPrimitive reg (TypeName "task")) (Just PCreate)
      t07 = expectEq "rootPrimitive discussion-turn = edit"
              (rootPrimitive reg (TypeName "discussion-turn")) (Just PEdit)

      -- Shape composition
      t08 = let fs = composedShape reg (TypeName "task")
                names = [fsName f | f <- fs]
            in expectEq "task composed shape includes create's fields"
                 names
                 [FieldName "title", FieldName "parent",
                  FieldName "content", FieldName "extra"]

      -- Primitive edit: missing required file
      t09 = let v = mkValue [("old_text", VString "foo"), ("new_text", VString "bar")]
            in expectLeft "edit missing file fails"
                 (validate reg kb (TypeName "edit") v)

      -- Primitive edit: file present but unresolved → passes primitive,
      -- shows that resolution is NOT a primitive-level check.
      t10 = let v = mkValue
                     [ ("file",     VString "nonexistent")
                     , ("old_text", VString "foo")
                     , ("new_text", VString "bar")
                     ]
            in expectRight "primitive edit with any file passes"
                 (validate reg kb (TypeName "edit") v)

      -- valid-edit: same value as above fails because of the dependent
      -- refinement that the file must resolve. This is the level-3 check.
      t11 = let v = mkValue
                     [ ("file",     VString "nonexistent")
                     , ("old_text", VString "foo")
                     , ("new_text", VString "bar")
                     ]
            in expectLeft "valid-edit with unresolved file fails"
                 (validate reg kb (TypeName "valid-edit") v)

      -- discussion-turn: suffix matches → passes
      t12 = let v = mkValue
                     [ ("file",     VString "Discussion - CI/CD Substrate")
                     , ("old_text", VString "The last line is this one.")
                     , ("new_text", VString "The last line is this one.\n\n**[agent]** reply")
                     ]
            in expectRight "discussion-turn with correct suffix passes"
                 (validate reg kb (TypeName "discussion-turn") v)

      -- discussion-turn: wrong suffix → fails
      t13 = let v = mkValue
                     [ ("file",     VString "Discussion - CI/CD Substrate")
                     , ("old_text", VString "wrong text")
                     , ("new_text", VString "doesn't matter")
                     ]
            in expectLeft "discussion-turn with wrong suffix fails"
                 (validate reg kb (TypeName "discussion-turn") v)

      -- task: extras present → passes
      t14 = let v = mkValue
                     [ ("title",   VString "My Task")
                     , ("content", VString "do the thing")
                     , ("extra",   VMap (Map.fromList
                                          [("status","todo"),("deadline","2026-05-01")]))
                     ]
            in expectRight "task with status+deadline passes"
                 (validate reg kb (TypeName "task") v)

      -- task: missing deadline → fails
      t15 = let v = mkValue
                     [ ("title",   VString "My Task")
                     , ("content", VString "do the thing")
                     , ("extra",   VMap (Map.fromList [("status","todo")]))
                     ]
            in expectLeft "task missing deadline fails"
                 (validate reg kb (TypeName "task") v)

      -- Level 4: dependent shape picker
      t16 = expectEq "discuss -> discussion-turn when discussion exists"
              (discussOutputType kb (Title "CI/CD Substrate"))
              (TypeName "discussion-turn")
      t17 = expectEq "discuss -> discussion-start when discussion absent"
              (discussOutputType kb (Title "Braindump"))
              (TypeName "discussion-start")

      -- discussion-start: enforces title prefix (level 1 refinement)
      t18 = let v = mkValue
                     [ ("title",   VString "Not A Discussion")
                     , ("content", VString "body")
                     ]
            in expectLeft "discussion-start with wrong title prefix fails"
                 (validate reg kb (TypeName "discussion-start") v)
      t19 = let v = mkValue
                     [ ("title",   VString "Discussion - Braindump")
                     , ("content", VString "body")
                     ]
            in expectRight "discussion-start with correct title passes"
                 (validate reg kb (TypeName "discussion-start") v)

  in [t01,t02,t03,t04,t05,t06,t07,t08,t09,t10,t11,t12,t13,t14,t15,t16,t17,t18,t19]

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
      putStrLn "--- Example schema instruction for discussion-turn ---"
      putStrLn (schemaInstruction exampleRegistry (TypeName "discussion-turn"))
      exitSuccess
    else do
      putStrLn $ show (length failed) ++ " of " ++ show (length rs) ++ " tests failed."
      exitFailure
