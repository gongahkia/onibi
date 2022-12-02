def calculator ():
    fhand = open ('input.txt', 'r')
    total = 0
    total_ribbon = 0
    for line in fhand:
        ribbon_length = 0
        pseudolist = []
        templist = []
        components = (line.rstrip('\n')).split ('x')
        for item in components:
            item = int (item)
            pseudolist.append (item)
        pseudolist.sort()
        print(pseudolist)
        print('break')
        l = int(pseudolist [0])
        w = int(pseudolist [1])
        h = int(pseudolist [2])
        lw = l * w
        wh = w * h
        hl = h * l
        templist.extend([lw,wh,hl])
        ribbon_length = (2 * l) + (2 * w) + (l * w * h)

    
        smallest_area = min(templist)
        total = smallest_area + (2 * lw) + (2 * wh) + (2 * hl) + total
        total_ribbon = total_ribbon + ribbon_length
    print (f'total ribbon length: {total_ribbon}')
    return f'wrapping paper required: {total}'

print(calculator())
