def readFile():
    fhand = open("example.txt", "r")
    for line in fhand:
        return line.strip()

def part1():
    line = readFile()
    first_char:str = ""
    last_char:str = ""
    sum:int = 0
    for index in range(len(line)):
        if index == 0:
            first_char = line[index]
        elif index == len(line)-1:
            last_char = line[index]
        elif int(line[index]) == int(line[index+1]): 
            sum += int(line[index])
        else:
            pass
    if last_char == first_char:
        sum += int(last_char)
    return sum

def part2():
    line = readFile()
    offset = int(len(line)/2)
    sum:int = 0
    curr_char:str= ""
    offset_char:str= ""
    for index in range(len(line)):
        curr_char = line[index]
        if offset + index > len(line)-1:
            offset_char = line[index+offset-len(line)]
        else:
            offset_char = line[index+offset]
        if curr_char == offset_char:
            sum += int(curr_char)
        else:
            pass
    return sum

print(f"part1: {part1()}")
print(f"part2: {part2()}")
