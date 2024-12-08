import qualified Data.Map as Map
import qualified Data.Set as Set
import Data.Char (isSpace)

type Coordinate = (Int, Int)
type HashMap = Map.Map Char [Coordinate]

parseGrid :: String -> IO ([[Char]], Int, Int)
parseGrid fileName = do
    content <- readFile fileName
    let grid = map (filter (not . isSpace)) (lines content)
        lengthGrid = length grid
        breadthGrid = if null grid then 0 else length (head grid)
    return (grid, lengthGrid, breadthGrid)

buildHashMap :: [[Char]] -> HashMap
buildHashMap grid = foldl addToMap Map.empty coords
  where
    coords = [((x, y), element) | (y, row) <- zip [0..] grid, (x, element) <- zip [0..] row, element /= '.']
    addToMap acc ((x, y), element) = Map.insertWith (++) element [(x, y)] acc

calculateAntinodesPart1 :: HashMap -> Int -> Int -> Int
calculateAntinodesPart1 hashmap breadth lengthGrid = Set.size antinodes
  where
    antinodes = Set.fromList 
      [ (newX, newY) 
      | (_, coords) <- Map.toList hashmap
      , (x1, y1) <- coords
      , (x2, y2) <- coords
      , (x1, y1) /= (x2, y2)
      , let newX = 2 * x1 - x2
      , let newY = 2 * y1 - y2
      , newX >= 0, newX < breadth, newY >= 0, newY < lengthGrid
      ]

linePoints :: Coordinate -> Coordinate -> [Coordinate]
linePoints (x1, y1) (x2, y2) =
    let dx = x2 - x1
        dy = y2 - y1
        step i = (x1 + i * dx, y1 + i * dy)
    in [step i | i <- [1, -1 .. max (abs dx) (abs dy)]]

calculateAntinodesPart2 :: HashMap -> Int -> Int -> Set.Set Coordinate
calculateAntinodesPart2 hashmap breadth lengthGrid = Set.fromList antinodes
  where
    antinodes = concatMap processCoordinates (Map.toList hashmap)
    processCoordinates (_, coords) = 
        [ (newX, newY)
        | (pos1:rest) <- [coords], pos2 <- rest
        , let line = linePoints pos1 pos2
        , (newX, newY) <- line
        , newX >= 0, newX < breadth, newY >= 0, newY < lengthGrid
        ]

main :: IO ()
main = do
    (grid, lengthGrid, breadth) <- parseGrid "input-1.txt"
    let hashmap = buildHashMap grid
    let result = calculateAntinodesPart1 hashmap breadth lengthGrid
    print result
    let antinodes = calculateAntinodesPart2 hashmap breadth lengthGrid
    print $ Set.size antinodes