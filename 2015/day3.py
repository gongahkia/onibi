class sled:

    def __init__ (self):
        print ('sled is instantiated!')
        self.location = [0,0]
        #1st coordinate is x-axis, 2nd coordinate is y-axis
    
    def north (self):
        self.location[1] = self.location[1] + 1

    def east (self):
        self.location[0] = self.location[0] + 1

    def south (self):
        self.location[1] = self.location[1] - 1

    def west (self):
        self.location[0] = self.location[0] - 1


def sled_planner (sled0,sled1):
    fhand = open ('input.txt','r')
    housecounter = {}
    condition = True

    for line in fhand:
        for input in line:
            if condition == True:
                if input == '^':
                    sled0.north()

                if input == '>':
                    sled0.east()

                if input == 'v':
                    sled0.south()

                if input == '<':
                    sled0.west()
                
                condition = False
                sled0.location1 = tuple(sled0.location)
                housecounter[sled0.location1] = housecounter.get(sled0.location1, 0) + 1
                
            else:
                if input == '^':
                    sled1.north()

                if input == '>':
                    sled1.east()

                if input == 'v':
                    sled1.south()

                if input == '<':
                    sled1.west()
            
                condition = True
                sled1.location1 = tuple(sled1.location)
                housecounter[sled1.location1] = housecounter.get(sled1.location1, 0) + 1

    return f'houses with at least one present: {len(housecounter)}'
    
            
            

fleshsanta = sled ()
metalsanta = sled ()
print(sled_planner(fleshsanta, metalsanta))