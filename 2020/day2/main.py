def part1():
    valid_counter = 0
    fhand = open("input.txt","r")
    for line in fhand:
        line = line.rstrip("\n")
        line = line.split(" ")
        lowerbound = int((line[0].split("-"))[0])
        upperbound = int((line[0].split("-"))[1])
        character = line[1].rstrip(":")
        password = line[2]
        count = password.count(character)
        print (character, password, count)
        if count >= lowerbound and count <= upperbound :
            valid_counter += 1
        count = 0
    return valid_counter

def part2():
    valid_counter = 0
    fhand = open("input.txt","r")
    for line in fhand:
        line = line.rstrip("\n")
        line = line.split(" ")
        index1 = int((line[0].split("-"))[0])
        index2 = int((line[0].split("-"))[1])
        character = line[1].rstrip(":")
        password = line[2]
        condition = 0
        if len(password) <= index2-1:
            continue
        if password[index1-1] == character:
            condition += 1
        if password[index2-1] == character:
            condition += 1
        if condition == 1:
            valid_counter += 1
    return valid_counter

print(part1())
print(part2())
