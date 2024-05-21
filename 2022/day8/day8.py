# True means visible 
# False means not visible
# to be updated later when looking via columns

coord:{(int):bool} = {}

def parta():
    fhand = open("input.txt","r")
    data_array:[int] = [line.rstrip() for line in fhand]
    fhand.close()
    
    for y in range(len(data_array)):
        for x in range(len(data_array[y])):
            if x > len(data_array[y]):
                break
            else:
                if data_array[y][x] >= data_array[y][x+1]:
                    coord[(x,y)] = True
                    coord[(x+1,y)] = False
                    break
                elif data_array[y][x] < data_array[y][x+1]:
                    coord[(x,y)] = True
                    continue
                else:
                    return "edge case 001"
    return coord
           
print(parta())
