-- ----- required imports -----

import Data.List (sort)
import Data.Map (fromListWith, (!?), Map)

-- ----- helper functions -----

totalDist :: [Int] -> [Int] -> Int
totalDist left right =
    let sortedLeft = sort left
        sortedRight = sort right
        distances = zipWith (\x y -> abs (x - y)) sortedLeft sortedRight
    in sum distances

simScore :: [Int] -> [Int] -> Int
simScore left right =
    let 
        rightCounts :: Map Int Int
        rightCounts = fromListWith (+) [(x, 1) | x <- right]
        score = sum [x * countOccurence x rightCounts | x <- left]
    in score

countOccurence :: Int -> Map Int Int -> Int
countOccurence x counts = case counts !? x of
    Just count -> count
    Nothing    -> 0

parse :: String -> ([Int], [Int])
parse content =
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
    -- content <- readFile "input-2.txt" 
    let (leftList, rightList) = parse content
    let partA = totalDist leftList rightList
    let partB = simScore leftList rightList
    putStrLn $ "part a total distance: " ++ show partA
    putStrLn $ "part b similarity score: " ++ show partB