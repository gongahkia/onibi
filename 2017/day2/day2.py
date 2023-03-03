def part1():
    fhand = open("input.txt","r")
    checksum = 0
    for line in fhand:
        templist = line.rstrip().split("\t")
        templist = [int(num) for num in templist]
        checksum += max(templist) - min(templist)
    return f"part 1: {checksum}"

def part2():
    fhand = open("input.txt", "r")
    checksum = 0
    for line in fhand:
        templist = line.rstrip().split("\t")
        templist = [int(num) for num in templist]
        for number in templist:
            for itervar in range(len(templist)):
                if number != templist[itervar] and number % templist[itervar] == 0:
                    checksum += number/templist[itervar]
    return f"part 2: {int(checksum)}"

print(part1())
print(part2())
