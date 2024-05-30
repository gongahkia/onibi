def readFile():
    tem:list[str] = []
    fin:list[list[str]] = []
    fhand = open("example3.txt", "r")
    for line in fhand:
        tem = line.strip().split(" ")
        fin.append(tem)
    fhand.close()
    return fin

def part1():
    invalidSum:int = 0
    for arr in readFile():
        for word in arr:
            if arr.count(word) > 1:
                invalidSum += 1
                break
    return len(readFile()) - invalidSum

def isAnagram(word1:str, word2:str):
    w1 = set(word1)
    w2 = set(word2)
    return w1 == w2

def part2():
    invalidSum = 0
    for arr in readFile():
        for word1 in arr:
            for word2 in arr:
                if word1 != word2:
                    if isAnagram(word1, word2):
                        invalidSum += 1
                        break
    return len(readFile()) - invalidSum

print(f"part1: {part1()}")
print(f"part2: {part2()}")
print(f"test:{isAnagram('thanks', 'sknaht')}")
