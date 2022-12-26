def part2 ():
    slice:int
    templist= []
    counter = 0
    prevslice = 0
    fhand = open("input.txt", "r")
    for line in fhand:
        templist.append(int(line.rstrip()))
    for itervar in range(len(templist)):
        if len(templist) - itervar == 2:
            break
        else:
            slice = sum(templist[itervar:itervar+3])
            if prevslice == 0:
                prevslice = slice
            elif slice > prevslice:
                counter += 1
                prevslice = slice
            else:
                prevslice = slice
    print (f"part 2 measurement counter: {counter}")

part2()