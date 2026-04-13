-- GoSubstrate.hs
--
-- A Haskell model of Go's type system, used as the substrate on which
-- the sevens type kernel (see TypesKernel.hs) gets implemented.
--
-- Run: ~/.ghcup/bin/runghc-9.6 docs/sketch/GoSubstrate.hs
--
-- The point of this file is to make Go's contribution explicit. Go is
-- the META LAYER -- it gives us closed structs, named types, methods,
-- and structural interface satisfaction. That is the entire substrate
-- for implementing the sevens kernel. Sevens types (primitives,
-- derived types, refinements) live at the OBJECT LAYER as runtime
-- data whose shape is described using Go structs and interfaces.
--
-- We model enough of Go to express the sevens kernel as a Go package
-- and check that the satisfaction relations hold before any Go gets
-- written. This is cheap compile-checking for the implementation
-- plan: does PrimitiveType satisfy TypeDef? Does Intrinsic/Contextual
-- Refinement both satisfy Refinement? Does BrokenRefinement correctly
-- fail to satisfy it?
--
-- What is modeled:
--   - Named types, closed structs with fixed field lists
--   - Methods attached to named types via receivers
--   - Interfaces as named method sets
--   - Structural satisfaction: T satisfies I iff method-set(T)
--     contains every signature in I
--
-- What is NOT modeled (because it does not affect the kernel design):
--   - Generics / type parameters
--   - Pointer-vs-value receivers (we treat method sets as flat)
--   - Embedding / method promotion
--   - Interface embedding beyond single-level
--   - Goroutines, channels, anything runtime-flavored

module Main where

import Data.Map.Strict (Map)
import qualified Data.Map.Strict as Map
import Data.List (sort, nub)
import System.Exit (exitFailure, exitSuccess)

--------------------------------------------------------------------------
-- Go type system
--------------------------------------------------------------------------

newtype GoName = GoName String deriving (Eq, Ord, Show)

-- A reference to a Go type. Just enough variety to express the kernel.
data GoType
  = GoInt
  | GoString
  | GoBool
  | GoError
  | GoNamed GoName            -- reference to a declared type
  | GoSlice GoType            -- []T
  | GoMap GoType GoType       -- map[K]V
  | GoFunc [GoType] [GoType]  -- func(params) returns
  deriving (Eq, Ord, Show)

data GoField = GoField
  { gfName :: String
  , gfType :: GoType
  } deriving (Eq, Show)

data GoStruct = GoStruct
  { gsName   :: GoName
  , gsFields :: [GoField]
  } deriving (Eq, Show)

-- A method signature (no receiver: the receiver is known from the
-- declaration that holds this signature).
data GoSig = GoSig
  { sigName    :: String
  , sigParams  :: [GoType]
  , sigReturns :: [GoType]
  } deriving (Eq, Ord, Show)

-- A method declaration: a signature attached to a receiver type.
data GoMethodDecl = GoMethodDecl
  { mdReceiver :: GoName
  , mdSig      :: GoSig
  } deriving (Eq, Show)

data GoInterface = GoInterface
  { giName    :: GoName
  , giMethods :: [GoSig]
  } deriving (Eq, Show)

-- A flat Go "package" environment.
data GoPkg = GoPkg
  { pkgStructs    :: Map GoName GoStruct
  , pkgInterfaces :: Map GoName GoInterface
  , pkgMethods    :: [GoMethodDecl]
  } deriving Show

emptyPkg :: GoPkg
emptyPkg = GoPkg Map.empty Map.empty []

addStruct :: GoStruct -> GoPkg -> GoPkg
addStruct s pkg = pkg { pkgStructs = Map.insert (gsName s) s (pkgStructs pkg) }

addInterface :: GoInterface -> GoPkg -> GoPkg
addInterface i pkg = pkg { pkgInterfaces = Map.insert (giName i) i (pkgInterfaces pkg) }

addMethod :: GoMethodDecl -> GoPkg -> GoPkg
addMethod m pkg = pkg { pkgMethods = m : pkgMethods pkg }

--------------------------------------------------------------------------
-- Method sets and structural satisfaction
--------------------------------------------------------------------------

-- Method set of a named type = every method declared on that receiver.
methodSet :: GoPkg -> GoName -> [GoSig]
methodSet pkg name = sort $ nub
  [ mdSig m | m <- pkgMethods pkg, mdReceiver m == name ]

-- Structural satisfaction: for each method required by the interface,
-- check that the type has a method with the same name, params, returns.
satisfies :: GoPkg -> GoName -> GoName -> Either String ()
satisfies pkg typeName ifaceName =
  case Map.lookup ifaceName (pkgInterfaces pkg) of
    Nothing -> Left ("no such interface: " ++ show ifaceName)
    Just iface ->
      let ms      = methodSet pkg typeName
          missing = [ needed | needed <- giMethods iface, needed `notElem` ms ]
      in case missing of
           [] -> Right ()
           _  -> Left ("type " ++ show typeName
                        ++ " does not satisfy " ++ show ifaceName
                        ++ "; missing: " ++ show (map sigName missing))

--------------------------------------------------------------------------
-- The sevens kernel expressed as a Go package
--
-- This is the compile-check. Every data type from TypesKernel.hs shows
-- up here as either a Go struct (data) or as a Go interface + structs
-- that satisfy it (sum types). If satisfaction holds, the implementation
-- plan holds.
--------------------------------------------------------------------------

sevensKernelPkg :: GoPkg
sevensKernelPkg = foldr ($) emptyPkg
  [ -- ---- Data structs ----------------------------------------------
    addStruct (GoStruct (GoName "TypeName")
      [ GoField "Value" GoString ])

  , addStruct (GoStruct (GoName "FieldSpec")
      [ GoField "Name"     GoString
      , GoField "Kind"     GoInt
      , GoField "Required" GoBool
      ])

  , addStruct (GoStruct (GoName "KB")
      [ GoField "Nodes" (GoMap GoString GoString) ])

  , addStruct (GoStruct (GoName "Value")
      [ GoField "Fields" (GoMap GoString (GoNamed (GoName "FieldValue"))) ])

  -- ---- Sum type: TypeDef -----------------------------------------
  --
  -- The `TypeDef` interface is how the Go implementation expresses
  -- the Haskell `TypeDef = PrimT | DerivedT` sum. Two concrete types
  -- satisfy it. The executor dispatches polymorphically through the
  -- interface.
  , addInterface (GoInterface (GoName "TypeDef")
      [ GoSig "Name"   []                                 [GoNamed (GoName "TypeName")]
      , GoSig "Parent" []                                 [GoNamed (GoName "TypeName")]
      , GoSig "Fields" []                                 [GoSlice (GoNamed (GoName "FieldSpec"))]
      ])

  , addStruct (GoStruct (GoName "PrimitiveType")
      [ GoField "Kind" GoInt ])
  , addMethod (GoMethodDecl (GoName "PrimitiveType")
      (GoSig "Name"   [] [GoNamed (GoName "TypeName")]))
  , addMethod (GoMethodDecl (GoName "PrimitiveType")
      (GoSig "Parent" [] [GoNamed (GoName "TypeName")]))
  , addMethod (GoMethodDecl (GoName "PrimitiveType")
      (GoSig "Fields" [] [GoSlice (GoNamed (GoName "FieldSpec"))]))

  , addStruct (GoStruct (GoName "DerivedType")
      [ GoField "TName"       (GoNamed (GoName "TypeName"))
      , GoField "ParentName"  (GoNamed (GoName "TypeName"))
      , GoField "ExtraFields" (GoSlice (GoNamed (GoName "FieldSpec")))
      , GoField "Refinements" (GoSlice (GoNamed (GoName "Refinement")))
      ])
  , addMethod (GoMethodDecl (GoName "DerivedType")
      (GoSig "Name"   [] [GoNamed (GoName "TypeName")]))
  , addMethod (GoMethodDecl (GoName "DerivedType")
      (GoSig "Parent" [] [GoNamed (GoName "TypeName")]))
  , addMethod (GoMethodDecl (GoName "DerivedType")
      (GoSig "Fields" [] [GoSlice (GoNamed (GoName "FieldSpec"))]))

  -- ---- Sum type: Refinement --------------------------------------
  --
  -- The Haskell `Refinement = Intrinsic | Contextual` becomes a Go
  -- interface whose Check method always takes a KB. Intrinsic refinements
  -- just ignore the KB argument -- that is the Go idiom for unifying the
  -- two kinds of refinement under one dispatchable type.
  , addInterface (GoInterface (GoName "Refinement")
      [ GoSig "Name"  [] [GoString]
      , GoSig "Check" [GoNamed (GoName "KB"), GoNamed (GoName "Value")]
                      [GoError]
      ])

  , addStruct (GoStruct (GoName "IntrinsicRefinement")
      [ GoField "NameStr" GoString
      , GoField "Fn"      (GoFunc [GoNamed (GoName "Value")] [GoError])
      ])
  , addMethod (GoMethodDecl (GoName "IntrinsicRefinement")
      (GoSig "Name"  [] [GoString]))
  , addMethod (GoMethodDecl (GoName "IntrinsicRefinement")
      (GoSig "Check" [GoNamed (GoName "KB"), GoNamed (GoName "Value")] [GoError]))

  , addStruct (GoStruct (GoName "ContextualRefinement")
      [ GoField "NameStr" GoString
      , GoField "Fn"      (GoFunc [GoNamed (GoName "KB"), GoNamed (GoName "Value")] [GoError])
      ])
  , addMethod (GoMethodDecl (GoName "ContextualRefinement")
      (GoSig "Name"  [] [GoString]))
  , addMethod (GoMethodDecl (GoName "ContextualRefinement")
      (GoSig "Check" [GoNamed (GoName "KB"), GoNamed (GoName "Value")] [GoError]))

  -- ---- Deliberate failure case -----------------------------------
  --
  -- A broken refinement where Check is missing the KB parameter. This
  -- models the exact mistake we would make if we forgot to give the
  -- parser a KB handle -- it should fail the satisfaction check so we
  -- notice it before writing Go.
  , addStruct (GoStruct (GoName "BrokenRefinement")
      [ GoField "NameStr" GoString ])
  , addMethod (GoMethodDecl (GoName "BrokenRefinement")
      (GoSig "Name"  [] [GoString]))
  , addMethod (GoMethodDecl (GoName "BrokenRefinement")
      (GoSig "Check" [GoNamed (GoName "Value")] [GoError]))  -- wrong sig
  ]

--------------------------------------------------------------------------
-- Tests
--------------------------------------------------------------------------

data TestResult = Pass String | Fail String String

showTest :: TestResult -> String
showTest (Pass name)       = "PASS  " ++ name
showTest (Fail name reason) = "FAIL  " ++ name ++ "\n        " ++ reason

expectRight :: String -> Either String () -> TestResult
expectRight name (Right ()) = Pass name
expectRight name (Left e)   = Fail name ("expected Right, got Left: " ++ e)

expectLeft :: String -> Either String () -> TestResult
expectLeft name (Left _)   = Pass name
expectLeft name (Right ()) = Fail name "expected Left, got Right"

tests :: [TestResult]
tests =
  let pkg = sevensKernelPkg
  in [ expectRight "PrimitiveType satisfies TypeDef"
         (satisfies pkg (GoName "PrimitiveType") (GoName "TypeDef"))
     , expectRight "DerivedType satisfies TypeDef"
         (satisfies pkg (GoName "DerivedType") (GoName "TypeDef"))
     , expectRight "IntrinsicRefinement satisfies Refinement"
         (satisfies pkg (GoName "IntrinsicRefinement") (GoName "Refinement"))
     , expectRight "ContextualRefinement satisfies Refinement"
         (satisfies pkg (GoName "ContextualRefinement") (GoName "Refinement"))
     , expectLeft "BrokenRefinement does NOT satisfy Refinement (wrong sig)"
         (satisfies pkg (GoName "BrokenRefinement") (GoName "Refinement"))
     , expectLeft "KB does NOT satisfy TypeDef (no methods)"
         (satisfies pkg (GoName "KB") (GoName "TypeDef"))
     , expectLeft "FieldSpec does NOT satisfy Refinement"
         (satisfies pkg (GoName "FieldSpec") (GoName "Refinement"))
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
      putStrLn ""
      putStrLn "--- Substrate shape ---"
      putStrLn $ "structs:    " ++ show (Map.size (pkgStructs sevensKernelPkg))
      putStrLn $ "interfaces: " ++ show (Map.size (pkgInterfaces sevensKernelPkg))
      putStrLn $ "methods:    " ++ show (length (pkgMethods sevensKernelPkg))
      exitSuccess
    else do
      putStrLn $ show (length failed) ++ " of " ++ show (length rs) ++ " tests failed."
      exitFailure
