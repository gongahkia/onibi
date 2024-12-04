import Data.List (elem)

readGrid :: IO [[Char]]
readGrid = fmap lines (readFile "input-1.txt")

n :: [[Char]] -> Int
n grid = length grid

isInBounds :: Int -> Int -> Int -> Int -> Bool
isInBounds n m i j = i >= 0 && i < n && j >= 0 && j < m

searchXmas :: [[Char]] -> Int
searchXmas grid = sum [searchAtPos grid i j | i <- [0..n-1], j <- [0..n-1], grid !! i !! j == 'X']
  where
    n = length grid
    searchAtPos grid i j = sum [if checkDirection i j di dj then 1 else 0 | di <- [-1, 0, 1], dj <- [-1, 0, 1], (di, dj) /= (0, 0)]
    checkDirection i j di dj = 
      all (\(x, y) -> isInBounds n n x y) positions &&
      all (\(x, y) -> (grid !! x) !! y == "XMAS" !! idx) positions
      where
        positions = [(i + di * k, j + dj * k) | k <- [0..3]]
        idx = 0

searchCrossMas :: [[Char]] -> Int
searchCrossMas grid = sum [if checkCrossMas grid i j then 1 else 0 | i <- [0..n-1], j <- [0..n-1], grid !! i !! j == 'A']
  where
    n = length grid
    checkCrossMas grid i j =
      isInBounds n n (i + 1) (j + 1) &&
      isInBounds n n (i + 1) (j - 1) &&
      isInBounds n n (i - 1) (j + 1) &&
      isInBounds n n (i - 1) (j - 1) &&
      (grid !! (i - 1) !! (j - 1), grid !! (i + 1) !! (j + 1)) `elem` [('M', 'S'), ('S', 'M')] &&
      (grid !! (i + 1) !! (j - 1), grid !! (i - 1) !! (j + 1)) `elem` [('M', 'S'), ('S', 'M')]

main :: IO ()
main = do
  grid <- readGrid
  putStrLn $ "part a: " ++ show(searchXmas grid)
  putStrLn$ "part b: " ++ show(searchCrossMas grid)