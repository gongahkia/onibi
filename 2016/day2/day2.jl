fhand = open("input.txt","r")
for line in fhand
    println(line)
end

function movement_up(coordinate)
    coordinate[1] = coordinate[1] + 1
end

function movement_down(coordinate)
    coordinate[1] = coordinate[1] - 1
end

function movement_left(coordinate)
    coordinate[0] = coordinate[0] - 1
end

function movement_right(coordinate)
    coordinate[0] = coordinate[0] + 1
end




