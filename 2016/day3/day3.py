def part1():
    fhand = open("input.txt", "r")
    count = 0
    for line in fhand:
        line = line.rstrip()
        components = []
        placeholder = line.split(" ")
        for word in placeholder:
            components.append(word.strip(" "))
        if int(components[0])>int(components[1])+int(components[2]):
            break
            #fail condition 
        elif int(components[1])>int(components[0])+int(components[2]):
            break
        elif int(components[2])>int(components[0])+int(components[1]):
            break
        else:
            count+=1
    return f"Part 1: {count}"
part1()
