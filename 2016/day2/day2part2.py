class movement:
    def __init__ (self, location = [0,0]):
        print("Movement object is instantiated")
        self.location = location
        #hashable object, list

    def up(self):
        self.location[1] += 1

    def down(self):
        self.location[1] -= 1

    def right(self):
        self.location[0] += 1

    def left(self):
        self.location[0] -= 1

    def checksum(self):
        if self.location == [0,3] or self.location == [-1,2] or self.location == [-2,1] or self.location == [-3,0] or self.location == [-2,-1] or self.location == [-1,-2] or self.location == [0,-3] or self.location == [1,-2] or self.location == [2,-1] or self.location == [3,0] or self.location == [2,1] or self.location == [1,2]:
            return True
        else:
            return False
        
def part2():
    templist = []
    fhand = open("input.txt","r")
    for line in fhand:
        line = line.rstrip("\n")
        templist.append(line)
    b = movement()
    for instruction in templist:
        for char in instruction:
            if char == 'U':
                b.up()
                if b.checksum() == True:
                    b.down()
            if char == 'D':
                b.down()
                if b.checksum() == True:
                    b.up()
            if char == 'L':
                b.left()
                if b.checksum() == True:
                    b.right()
            if char == 'R':
                b.right()
                if b.checksum() == True:
                    b.left()
        print(b.location)

part2()
