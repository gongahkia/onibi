def prep_data(file_name) -> [[]]:
    fhand = open(file_name, 'r')
    total_array = []
    for line in fhand:
        temp_array = [int(num) for num in line.split()]
        total_array.append(temp_array) 
    fhand.close()
    return total_array

def create_log_file(output, file_name):
    fhand = open(file_name, 'a')
    fhand.write(output)
    fhand.close()

def compare_array(arr_1, arr_2) -> bool:
    for i in range(len(arr_1)):
        if arr_1[i] != arr_2[i]:
            return False
    return True

def part_1():
    temp = prep_data('./input-1.txt')
    count = 0
    for line in temp:
        line2 = sorted(line) # sort by ascending
        line3 = sorted(line, reverse=True) # sort by descending
        if compare_array(line, line2) or compare_array(line, line3):
            for i in range(len(line)-1):
                if not 1 <= abs(line[i] - line[i+1]) <= 3:
                    create_log_file("unsafe", "log.txt")
                    create_log_file(", ".join([str(x) for x in line]), "log.txt")
                    break
            else:
                create_log_file("safe", "log.txt")
                create_log_file(", ".join([str(x) for x in line]), "log.txt")
                count += 1
        else: 
            create_log_file("unsafe", "log.txt")
            create_log_file(", ".join([str(x) for x in line]), "log.txt")
            continue
        create_log_file("\n", "log.txt")
    return count

print(part_1())
