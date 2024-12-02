import Data.List (tails)

parse :: String -> [Int]
parse = map read . words

consistent :: [Int] -> Bool
consistent xs = inc || dec
  where
    diffs = zipWith (-) (tail xs) xs
    inc = all (>= 1) diffs && all (<= 3) diffs
    dec = all (<= -1) diffs && all (>= -3) diffs

safe :: [Int] -> Bool
safe = consistent

safeWithDamp :: [Int] -> Bool
safeWithDamp xs
    | safe xs = True
    | otherwise = any safe (removeOne xs)
  where
    removeOne ys = [take i ys ++ drop (i + 1) ys | i <- [0 .. length ys - 1]]

countSafeA :: [String] -> Int
countSafeA = length . filter (safe . parse)

countSafeB :: [String] -> Int
countSafeB = length . filter (safeWithDamp . parse)

main :: IO ()
main = do
    input <- readFile "input-1.txt"
    let reports = lines input
    putStrLn $ "part a safe reports: " ++ show (countSafeA reports)
    putStrLn $ "part b safe reports: " ++ show (countSafeB reports)