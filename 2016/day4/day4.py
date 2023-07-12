def parse_input(file_name):
    fhand = open(file_name)
    sum_sector_id = 0 
    for line in fhand:
        overall_count = {}
        line = line.strip()
        temp_array = line.split("-")
        final_array = temp_array[:-1]
        sector_id = int(temp_array[-1].split("[")[0])
        checksum = temp_array[-1].split("[")[1].rstrip("]")
        # print(temp_array)
        print("--------------------")
        # print(final_array)
        print(f"checksum = {checksum}")
        print(f"sector ID = {sector_id}")
        for word in final_array:
            # print("--")
            for char in word:
                # print(char)
                overall_count[char] = overall_count.get(char,0) + 1
        temp_count_tuple = [(count, letter) for letter, count in overall_count.items()]
        print(overall_count)
        print(temp_count_tuple)
        for i in range(0,len(temp_count_tuple)):
            print(temp_count_tuple[i])
            if temp_count_tuple[i][0] > temp_count_tuple[i+1][0]:
                print(temp_count_tuple)

parse_input("input2.txt")