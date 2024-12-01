-- ----- required imports -----

import Data.List (sort)
import Data.Map (fromListWith, (!?), Map)

-- ----- helper functions -----

totalDistance :: [Int] -> [Int] -> Int
totalDistance left right =
    let sortedLeft = sort left
        sortedRight = sort right
        distances = zipWith (\x y -> abs (x - y)) sortedLeft sortedRight
    in sum distances

similarityScore :: [Int] -> [Int] -> Int
similarityScore left right =
    let 
        rightCounts :: Map Int Int
        rightCounts = fromListWith (+) [(x, 1) | x <- right]
        score = sum [x * countOccurrences x rightCounts | x <- left]
    in score

countOccurrences :: Int -> Map Int Int -> Int
countOccurrences x counts = case counts !? x of
    Just count -> count
    Nothing    -> 0

parseInput :: String -> ([Int], [Int])
parseInput content =
    let linesOfInput = lines content
        splitNumbers line = case words line of
            [a, b] -> (read a, read b)
            _      -> error "Invalid line format"
        pairs = map splitNumbers linesOfInput
    in (map fst pairs, map snd pairs)

-- ----- actual execution code -----

main :: IO ()
main = do
    content <- readFile "input-1.txt"
    -- content <- readFile "input-2.txt" -- for debugging
    let (leftList, rightList) = parseInput content
    let partAResult = totalDistance leftList rightList
    let partBResult = similarityScore leftList rightList
    putStrLn $ "part a total distance: " ++ show partAResult
    putStrLn $ "part b similarity score: " ++ show partBResult