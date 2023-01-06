def part1():
    column_storer = {}
    gamma_rate = ''
    epsilon_rate = ''
    fhand = open("input.txt","r")
    for line in fhand:
        line = line.rstrip("\n")
        count = 0
        for char in line:
            column_storer[count] = column_storer.get(count,"") + char 
            count += 1
    for columnnum, contents in column_storer.items():
        counter = {}
        for char in contents:
            counter[char] = counter.get(char,0) + 1
        if counter['0'] > counter['1']:
            gamma_rate += '0'
            epsilon_rate += '1'
        else:
            gamma_rate += '1'
            epsilon_rate += '0'
    return (int(gamma_rate,2) * int(epsilon_rate,2))

def Oxygen():
    i = 0
    Oxygendecimal = 0
    data_storer = []
    fhand = open("input.txt","r")
    for line in fhand:
        line = line.rstrip("\n")
        data_storer.append(line)

    Oxygen_data_storer = data_storer

    #recurse until the number of items in data_storer == 1, and edit the list directly to save changes across iterations
    for char in Oxygen_data_storer[0]:

        if len(Oxygen_data_storer) == 1:
            break

        Oxygen_toremove: string
        column_storer = {}

        #creation of column per iteration to track more common/less common bits
        for row in Oxygen_data_storer:
            count = 0
            for char in row:
                column_storer[count] = column_storer.get(count,"") + char
                count += 1

        indent = 0

        if column_storer[i].count("0") > column_storer[i].count("1"):
            Oxygen_toremove = "1"
        elif column_storer[i].count("1") > column_storer[i].count("0"):
            Oxygen_toremove = "0"
        else:
            Oxygen_toremove = "0"
        
        if Oxygen_toremove == "1":
            for itervar in range(len(column_storer[i])):
                if (column_storer[i])[itervar] == "1":
                    del Oxygen_data_storer[itervar-indent]
                    indent += 1

        elif Oxygen_toremove == "0":
            for itervar in range(len(column_storer[i])):
                if (column_storer[i])[itervar] == "0":
                    del Oxygen_data_storer[itervar-indent]
                    indent += 1

        else:
            print("Edge case")

        i += 1

    Oxygendecimal = int(Oxygen_data_storer[0],2)
    return Oxygendecimal



def CO2():
    q = 0
    Carbondecimal = 0
    data_storer = []
    fhand = open("input.txt","r")
    for line in fhand:
        line = line.rstrip("\n")
        data_storer.append(line)

    Carbon_data_storer = data_storer

    for char in Carbon_data_storer[0]:
        
        if len(Carbon_data_storer) == 1:
            break

        Carbon_toremove: string
        column_storer = {}

        for row in Carbon_data_storer:
            count = 0
            for char in row:
                column_storer[count] = column_storer.get(count,"") + char
                count += 1

        carbonindent = 0 
        
        if column_storer[q].count("0") > column_storer[q].count("1"):
            Carbon_toremove = "0"
        elif column_storer[q].count("1") > column_storer[q].count("0"):
            Carbon_toremove = "1"
        else:
            Carbon_toremove = "1"
 
        if Carbon_toremove == "1":
            for itervar in range(len(column_storer[q])):
                if (column_storer[q])[itervar] == "1":
                    del Carbon_data_storer[itervar-carbonindent]
                    carbonindent += 1

        elif Carbon_toremove == "0":
            for itervar in range(len(column_storer[q])):
                if (column_storer[q])[itervar] == "0":
                    del Carbon_data_storer[itervar-carbonindent]
                    carbonindent += 1

        else:
            print("Edge case")
        
        q += 1


    Carbondecimal = int(Carbon_data_storer[0],2)
    
    return (Carbondecimal)

def part2():
    return (Oxygen() * CO2())

print(f"part 1: {part1()}")
print(f"part 2: {part2()}")
