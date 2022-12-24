def part2 ():
    fhand = open ("input.txt","r")
    totalsum = 0
    for line in fhand:
        temppsum = 0
        #print (line)
        tempsum = int(line)//3 -2
        while tempsum > 0:
            temppsum += tempsum
            tempsum = int(tempsum)//3 -2
        totalsum += temppsum
    return (totalsum)
    
print(part2())