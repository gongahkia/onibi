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

    
            











print(part1())
