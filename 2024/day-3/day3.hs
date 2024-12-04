import Text.Regex.PCRE ((=~))
import Data.Maybe (mapMaybe)

-- Extracts all valid mul instructions and computes their results
parseAndSumMul :: String -> Int
parseAndSumMul input =
    let regex = "mul\\(([0-9]+),([0-9]+)\\)" -- Matches "mul(X,Y)" with digits X and Y
        matches = input =~ regex :: [[String]] -- Perform regex match
        extractProduct [_, x, y] = Just (read x * read y) -- Compute product
        extractProduct _ = Nothing
        results = mapMaybe extractProduct matches -- Filter and compute valid results
    in sum results -- Sum up all the results

main :: IO ()
main = do
    input <- getContents -- Read the corrupted memory input
    let result = parseAndSumMul input
    print result
