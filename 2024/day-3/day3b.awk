BEGIN { sum = 0; enabled = 1 }
{
    while (match($0, /(do\(\)|don't\(\)|mul\(([0-9]+),([0-9]+)\))/, groups)) {
        instruction = groups[1]
        if (instruction == "do()") enabled = 1
        else if (instruction == "don't()") enabled = 0
        else if (enabled && instruction ~ /^mul\(([0-9]+),([0-9]+)\)$/) sum += groups[2] * groups[3]
        $0 = substr($0, RSTART + RLENGTH)
    }
}
END { print sum }