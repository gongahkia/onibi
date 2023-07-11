def parse_input_part1(file_name):
    fhand = open(file_name)
    final_array = []
    for line in fhand:
        line = line.rstrip("\n")
        temporary_array = line.split("  ")
        next_temporary_array = []
        for element in temporary_array:
            element = element.strip()
            if element == "":
                pass
            else:
                element = int(element)
                next_temporary_array.append(element)
        final_array.append(next_temporary_array)
    return final_array

def part1(file_name):
    num_possible = 0
    length_array = parse_input_part1(file_name)
    print(length_array)
    for length in length_array:
        length.sort()
        the_sum = length[0] + length[1]
        if the_sum > length[2]:
            #print(length)
            num_possible += 1
        elif the_sum <= length[2]:
            pass
        else:
            print("--------------------\nedge case detected\n--------------------")
            print(length)
            break
    return num_possible


def parse_input_part2(file_name):
    # just initial parsing things
    fhand = open(file_name)
    final_array = []
    output_array = []
    for line in fhand:
        lengths = line.split(" ")
        temporary_array = []
        for element in lengths:
            element = element.strip()
            if element == " " or element == "":
                pass
            else:
                temporary_array.append(int(element))
        final_array.append(temporary_array)
    print(final_array)
    # --------------------
    reset_count = 0
    triangle1_array = []
    triangle2_array = []
    triangle3_array = []
    for line in final_array:
        if reset_count == 3:
            print("--------------------")
            print(f"triangle 1 = {triangle1_array}")
            print(f"triangle 2 = {triangle2_array}")
            print(f"triangle 3 = {triangle3_array}")
            print("--------------------")
            output_array.append(triangle1_array)
            output_array.append(triangle2_array)
            output_array.append(triangle3_array)
            triangle1_array = []
            triangle2_array = []
            triangle3_array = []
            reset_count = 0
            reset_count += 1
            print(f"reset count = {reset_count}")
            print(line)
            for i in range(0,3):
                if i == 0:
                    print(line[i])
                    triangle1_array.append(line[i])
                elif i == 1:
                    print(line[i])
                    triangle2_array.append(line[i])
                elif i == 2:
                    print(line[i])
                    triangle3_array.append(line[i])
                else:
                    print("edge case detected")
        else:
            reset_count += 1
            print(f"reset count = {reset_count}")
            print(line)
            # print(line[2])
            for i in range(0,3):
                if i == 0:
                    print(line[i])
                    triangle1_array.append(line[i])
                elif i == 1:
                    print(line[i])
                    triangle2_array.append(line[i])
                elif i == 2:
                    print(line[i])
                    triangle3_array.append(line[i])
                else:
                    print("edge case detected")
    print("--------------------")
    print(f"triangle 1 = {triangle1_array}")
    print(f"triangle 2 = {triangle2_array}")
    print(f"triangle 3 = {triangle3_array}")
    print("--------------------")
    output_array.append(triangle1_array)
    output_array.append(triangle2_array)
    output_array.append(triangle3_array)
    return output_array
    # print(f"FINAL OUTPUT ARRAY = {output_array}")
    
    
def part2(file_name):
    num_possible = 0
    length_array = parse_input_part2(file_name)
    print(length_array)
    for length in length_array:
        print(length)
        length.sort()
        the_sum = length[0] + length[1]
        if the_sum > length[2]:
            print(length)
            num_possible += 1
        elif the_sum <= length[2]:
            pass
        else:
            print("--------------------\nedge case detected\n--------------------")
            print(length)
            break
    return num_possible

file_name = "input.txt"
print(part1(file_name))
print(part2(file_name))
