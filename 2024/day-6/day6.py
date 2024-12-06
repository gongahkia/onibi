def parse(target_filepath):
    try:
        with open(target_filepath, 'r') as file:
            input_map = [line.strip() for line in file]
    except FileNotFoundError:
        print("file not found")
        input_map = []
    finally:
        return input_map

def sanitise(input_map):
    return [list(row) for row in input_map]

def simulation(grid):
    dir_map = {'^': (-1, 0), '>': (0, 1), 'v': (1, 0), '<': (0, -1)}
    rturn_map = {'^': '>', '>': 'v', 'v': '<', '<': '^'}
    rows, cols = len(grid), len(grid[0])
    pos = None
    facing = None
    for r in range(rows):
        for c in range(cols):
            if grid[r][c] in dir_map:
                pos = (r, c)
                facing = grid[r][c]
                break
        if pos:
            break
    visited = set()
    visited.add(pos)
    while True:
        dr, dc = dir_map[facing]
        next_pos = (pos[0] + dr, pos[1] + dc)
        if not (0 <= next_pos[0] < rows and 0 <= next_pos[1] < cols):
            break
        if grid[next_pos[0]][next_pos[1]] == '#':
            facing = rturn_map[facing]
        else:
            pos = next_pos
            visited.add(pos)
    return len(visited)

if __name__ == "__main__":
    print(f'part a: {simulation(parse("input-1.txt"))}')