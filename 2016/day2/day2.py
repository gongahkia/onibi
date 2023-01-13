class movement:

    def __init__(self, coord = [0,0]):
        print("Movement body is instantiated!")
        self.coordinates = coord

    def up(self):
        self.coordinates[1] += 1
    
    def down(self):
        self.coordinates[1] -= 1

    def left(self):
        self.coordinates[0] -= 1

    def right(self):
        self.coordinates[0] += 1

    def checkbounds(self):
        if self.coordinates[0] < -1 or self.coordinates[0] > 1:
            return True
        if self.coordinates[1] < -1 or self.coordinates[1] > 1:
            return True
        else:
            return False

def part1():
    templist = []
    fhand = open("input.txt","r")
    for line in fhand:
        line = line.rstrip("\n")
        templist.append(line)
    
    a = movement()

    for item in templist:
        for char in item:
            if char == "D":
                a.down()
                if a.checkbounds() == True:
                    a.up()
            elif char == "U":
                a.up()
                if a.checkbounds() == True:
                    a.down()
            elif char == "L":
                a.left()
                if a.checkbounds() == True:
                    a.right()
            elif char == "R":
                a.right()
                if a.checkbounds() == True:
                    a.left()
        print(a.coordinates)
        #dictionaries weren't working for me today, no idea why tbh

part1()
