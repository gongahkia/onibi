import Data.List

parse :: String -> ([(Int, Int, Int)], [(Int, Int)])
parse disk = parse' 0 disk [] []
  where
    parse' _ [] f free = (reverse f, reverse free)
    parse' pos (x:xs) f free = 
        let size = read [x]
        in if even pos
           then parse' (pos + 1) xs ((pos `div` 2, pos, size):f) free
           else parse' (pos + 1) xs f ((pos, size):free)

proc :: String -> [Int]
proc disk = proc' (concatMap (\(i, c) -> replicate (read [c]) (if i `mod` 2 == 0 then i `div` 2 else -1)) (zip [0..] disk))
  where
    proc' [] = []
    proc' (x:xs)
        | x == -1  = proc' xs
        | otherwise = x : proc' xs

partA :: IO Int
partA = do
    disk <- readFile "input-1.txt"
    let layout = proc disk
    let result = sum $ zipWith (*) [0..] layout
    return result

partB :: IO Int
partB = do
    disk <- readFile "input-1.txt"
    let (f, free) = parse disk
    let result = sum $ map (\(fid, fpos, fsize) -> fromIntegral (fpos + fid * fsize)) f
    return result

cleanFree :: [(Int, Int)] -> [(Int, Int)]
cleanFree free = foldl' merge [] (sortOn fst free)
  where
    merge [] f = [f]
    merge (x:xs) (fpos, fsize)
        | fst x + snd x == fpos = (fst x, snd x + fsize):xs
        | otherwise = (fpos, fsize):x:xs

main :: IO ()
main = do
    resultA <- partA
    resultB <- partB
    putStrLn $ "part a: " ++ show resultA
    putStrLn $ "part b: " ++ show resultB