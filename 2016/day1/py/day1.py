def parse_instructions(file_name):
    fhand = open(file_name)
    for line in fhand:
        instruction_array = line.split(",")
    final_instruction_array = []
    for instruction in instruction_array:
        instruction = instruction.strip()
        final_instruction_array.append(instruction)
    return final_instruction_array

# -----------

def part1(file_name):
    instruct_array = parse_instructions(file_name)
    print(instruct_array)

# initial azimuth 0 north, 90 east, 180 south, 270 west --> 0 north, 1 east, 2 south, 3 west
    azimuth = 0
    horizontal_displacement = 0
    vertical_displacement = 0

    for movement in instruct_array:
        # print(movement) # -- for debugging
        if movement.startswith("L"):
            azimuth -= 90
            current_azimuth = (abs(azimuth)%360)/90
            print("left")
            match current_azimuth:
                #north 
                case 0:
                    vertical_displacement += int(movement[1:])

                #east
                case 1:
                    horizontal_displacement += int(movement[1:])

                #south
                case 2:
                    vertical_displacement -= int(movement[1:])

                #west
                case 3:
                    horizontal_displacement -= int(movement[1:])

                case _:
                    print("default edge case")
                    break

        elif movement.startswith("R"):
            azimuth += 90
            current_azimuth = (abs(azimuth)%360)/90
            print("right")
            match current_azimuth:
                #north 
                case 0:
                    vertical_displacement += int(movement[1:])

                #east
                case 1:
                    horizontal_displacement += int(movement[1:])

                #south
                case 2:
                    vertical_displacement -= int(movement[1:])

                #west
                case 3:
                    horizontal_displacement -= int(movement[1:])

                case _:
                    print("default edge case")
                    break
        else:
            print("edge case unaccounted for")
            print(movement)

    final_displacement = abs(horizontal_displacement) + abs(vertical_displacement)
    return final_displacement

print(part1("input.txt"))
