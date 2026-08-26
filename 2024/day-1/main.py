def prep_file(file_name) -> [[]]:
    fhand = open(file_name , "r")
    list1 = []
    list2 = []
    for line in fhand:
        try: 
            list1.append(int(line.split("   ")[0].strip()))
            list2.append(int(line.split("   ")[1].strip()))
        except: 
            pass
    fhand.close()
    return [list1, list2]

def part1():
    one = prep_file("./input-1.txt")
    tot = 0
    list1, list2 = one[0], one[1]
    list1.sort() 
    list2.sort()
    for i in range(0, len(list1)):
        tot += abs(list1[i] - list2[i])
    return tot

def part2():
    two = prep_file("./input-1.txt")
    tot = 0
    list1, list2 = two[0], two[1]
    list1.sort() 
    list2.sort()
    for i in range(0, len(list1)):
        tot += list1[i] * list2.count(list1[i])
    return tot

print(part1())
print(part2())
