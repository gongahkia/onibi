def floor_calculator ():
    fhand = open ('input.txt','r')
    floornum = 0
    counter = 0
    for direction in fhand:
        for instruction in direction:
            if floornum == -1:
                break
            if instruction == '(':
                floornum = floornum + 1
            else:
                floornum = floornum - 1
            counter = counter + 1

    print (f'character position: {counter}')
    return floornum


print(floor_calculator())
