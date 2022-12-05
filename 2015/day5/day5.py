def naughty_or_nice1 ():
    fhand = open ('input.txt', 'r')
    output = 0
    for line in fhand:
        condition, templist, character_holder, tempdict, total = 0, [], '', {}, 0
        line = line.rstrip('\n')
        if 'ab' in line or 'cd' in line or 'pq' in line or 'xy' in line:
            continue
        #condition 3: string check

        for words in line:
            templist.append (words)

        for character in templist:
            tempdict [character] = tempdict.get(character, 0) + 1
        if 'a' in tempdict:
            total += tempdict ['a'] 
        if 'e' in tempdict:
            total += tempdict ['e'] 
        if 'i' in tempdict:
            total += tempdict ['i'] 
        if 'o' in tempdict:
            total += tempdict ['o'] 
        if 'u' in tempdict:
            total += tempdict ['u'] 
        if total >= 3:
            condition += 1 
        #condition 1: vowel check

        for itervar in range(len(templist)):
            if character_holder == '':
                character_holder = templist[itervar]
                continue
            if templist[itervar] == character_holder:
                condition += 1
                break
            else:
                character_holder = templist[itervar]
        #condition 2: repeated character check
        
        if condition == 2:
            output += 1
            
    return f'part 1 output: {output}'


def naughty_or_nice2 ():
    fhand = open ('input.txt', 'r')
    output = 0
    for line in fhand:
        templist = []
        permlist = []
        condition = 0
        line = line.rstrip('\n')
        for itervar in range(len(line)):
            char = line [itervar:itervar+2]
            if len (char) != 2:
                break

            count = line.count(char)

            if count >=2:
                condition += 1

            else:
                continue
            #print (f'count: {count}')
            #print (f'condition 1: {condition}')
            break
        #condition 1: repeated string 

        for words in line:
            templist.append (words)    

        for itervar in range(len(templist)):
            word = templist[itervar:itervar+3]
            permlist.append(word)
        
        for group in permlist:
            if len (group) != 3:
                break
            if group[0] == group[2]:
                    condition += 1
                    break
            else:
                pass

        if condition == 2:
            output +=1
        #print (f'total output: {output}')

    return f'part 2 output: {output}'            

print(naughty_or_nice1())
print(naughty_or_nice2())