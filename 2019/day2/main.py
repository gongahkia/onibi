def fileparser(noun,verb):
    fhand = open("input.txt","r")
    for line in fhand:
        templist = line.split(",")
    templist[1],templist[2] = noun,verb
    return templist

def day2part1(noun,verb):
    opcode = []
    baselist = []
    product:int

    templist = fileparser(noun,verb)
    baselist = templist
    
    for itervar in range(0,len(templist),4):
        opcode = templist[itervar:itervar+4]

        if int(opcode[0]) == 1:
            #addition
            product = int(baselist[int(opcode[1])]) + int(baselist[int(opcode[2])])
            baselist[int(opcode[3])] = product

        elif int(opcode[0]) == 2:
            #multiplication
            product = int(baselist[int(opcode[1])]) * int(baselist[int(opcode[2])])
            baselist[int(opcode[3])] = product

        elif int(opcode[0]) == 99:
            #immediate halt condition
            break
        
    return baselist[0]

def day2part2():
    for noun in range(0,100):
        for verb in range(0,100):
            if day2part1(noun,verb) == 19690720:
                return (noun,verb)
            
print(day2part1(12,2))
print(day2part2())
