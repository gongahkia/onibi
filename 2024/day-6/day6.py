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

def simulate_movement(grid):
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

def is_loop(grid_with_obstacle, pos, facing, rows, cols, dir_map, rturn_map):
    visited = {}
    curr_pos = pos
    current_facing = facing
    steps = 0  
    max_steps = rows * cols * 2 # arbitrary limit
    while steps < max_steps:
        state = (curr_pos, current_facing)
        if state in visited:
            return True  
        visited[state] = steps
        dr, dc = dir_map[current_facing]
        next_pos = (curr_pos[0] + dr, curr_pos[1] + dc)
        if not (0 <= next_pos[0] < rows and 0 <= next_pos[1] < cols):
            return False  
        if grid_with_obstacle[next_pos[0]][next_pos[1]] == '#':
            current_facing = rturn_map[current_facing]
        else:
            curr_pos = next_pos
        steps += 1
    return False  

def find_obstructed_loops(grid):
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
    valid_positions = 0
    for r in range(rows):
        for c in range(cols):
            if (r, c) == pos or grid[r][c] == '#':
                continue
            grid[r][c] = '#'
            if is_loop(grid, pos, facing, rows, cols, dir_map, rturn_map):
                valid_positions += 1
            grid[r][c] = '.'
    return valid_positions

if __name__ == "__main__":
    print(f'part a: {simulate_movement(parse("input-1.txt"))}')
    print(f'part b: {find_obstructed_loops(sanitise(parse("input-1.txt")))}')