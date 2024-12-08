import qualified Data.Map as Map
import qualified Data.Set as Set
import Data.Char (isSpace)

type Coordinate = (Int, Int)
type HashMap = Map.Map Char [Coordinate]

parseGrid :: String -> IO ([[Char]], Int, Int)
parseGrid fileName = do
    content <- readFile fileName
    let grid = map (filter (not . isSpace)) (lines content)
    return (grid, length grid, length (head grid))

buildHashMap :: [[Char]] -> HashMap
buildHashMap grid = foldl (\m (x, y, e) -> Map.insertWith (++) e [(x, y)] m) Map.empty coords
  where coords = [(x, y, e) | (y, row) <- zip [0..] grid, (x, e) <- zip [0..] row, e /= '.']

calcAnti1 :: HashMap -> Int -> Int -> Int
calcAnti1 hashmap breadth lengthGrid = Set.size $ Set.fromList
    [ (2 * x1 - x2, 2 * y1 - y2)
    | (_, coords) <- Map.toList hashmap
    , (x1, y1) <- coords
    , (x2, y2) <- coords, (x1, y1) /= (x2, y2)
    , let (newX, newY) = (2 * x1 - x2, 2 * y1 - y2)
    , newX >= 0, newX < breadth, newY >= 0, newY < lengthGrid
    ]

linePoints :: Coordinate -> Coordinate -> [Coordinate]
linePoints (x1, y1) (x2, y2) = [(x1 + i * dx, y1 + i * dy) | i <- [1, -1 .. max (abs dx) (abs dy)]]
  where (dx, dy) = (x2 - x1, y2 - y1)

calcAnti2 :: HashMap -> Int -> Int -> Set.Set Coordinate
calcAnti2 hashmap breadth lengthGrid = Set.fromList
    [ (newX, newY) | (_, coords) <- Map.toList hashmap
    , (pos1:rest) <- [coords], pos2 <- rest
    , (newX, newY) <- linePoints pos1 pos2
    , newX >= 0, newX < breadth, newY >= 0, newY < lengthGrid
    ]

main :: IO ()
main = do
    (grid, lengthGrid, breadth) <- parseGrid "input-1.txt"
    let hashmap = buildHashMap grid
    putStrLn $ "part a: " ++ show (calcAnti1 hashmap breadth lengthGrid)
    putStrLn $ "part b: " ++ show (Set.size $ calcAnti2 hashmap breadth lengthGrid)