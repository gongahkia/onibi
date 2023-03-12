fhand = open("input.txt", 'r')
line_dict, two_count, three_count = {}, 0, 0

for line in fhand:
    line = line.rstrip()
    for char in line:
        line_dict[char] = line_dict.get(char, 0) + 1
    for character, count in line_dict.items():
        if count == 2:
            two_count += 1
            break
    for character, count in line_dict.items():
        if count == 3:
            three_count += 1
            break
    line_dict = {}

print (f"part1: {two_count * three_count}")

fhand = open("input.txt", 'r')
overall_list = []
for line in fhand:
    overall_list.append(line.rstrip())

for itervar in range(len(overall_list)):
    counter = 0
    tolerance = 0
    while tolerance < 3:
        if counter > len(overall_list[itervar]):
            break
        if overall_list[itervar][counter] == overall_list[itervar+1][counter]:
            pass
        else:
            tolerance += 1
        counter += 1