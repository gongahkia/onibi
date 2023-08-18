# square UP from access port of displacement value Y + 1
# increment of 8, starting diff value is 3
    # * 3 
    # * 11
    # * 19
    # * 27 

# square DOWN from access port of displacement value Y - 1
# increment of 8, starting diff value is 7
    # * 7
    # * 15
    # * 23
    # * 31

# square LEFT from access port of displacement value X - 1
# increment of 8, starting diff value is 5
    # 5
    # 13 
    # 21
    # 29

# square RIGHT from access port of displacement value X + 1
# increment of 8, starting diff value is 1
    # 1
    # 9
    # 17
    # 25

# ~TEST CASES~

# Data from square 1 is carried 0 steps, since it's at the access port.
# Data from square 12 is carried 3 steps, such as: down, left, left.
# Data from square 23 is carried only 2 steps: up twice.
# Data from square 1024 must be carried 31 steps.

# ---

fhand = open('input.txt', 'r')
for line in fhand:
    input = line.rstrip()

def part1(cell_num):

part1(input)
