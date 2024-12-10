from collections import deque

def parse(file_path):
    with open(file_path, 'r') as file:
        return [[int(x) for x in line.strip()] for line in file]

def is_valid(x, y, n, m):
    return 0 <= x < n and 0 <= y < m

def bfs(map_grid, n, m, start_x, start_y, part=1):
    directions = [(-1, 0), (1, 0), (0, -1), (0, 1)]
    queue = deque([(start_x, start_y)])
    visited = set([(start_x, start_y)])
    if part == 1:
        score = 0
        while queue:
            x, y = queue.popleft()
            if map_grid[x][y] == 9: score += 1
            for dx, dy in directions:
                nx, ny = x + dx, y + dy
                if is_valid(nx, ny, n, m) and (nx, ny) not in visited and map_grid[nx][ny] == map_grid[x][y] + 1:
                    queue.append((nx, ny))
                    visited.add((nx, ny))
        return score
    else:
        trails = set()
        queue = deque([(start_x, start_y, [(start_x, start_y)])])
        while queue:
            x, y, path = queue.popleft()
            if map_grid[x][y] == 9: trails.add(tuple(path))
            for dx, dy in directions:
                nx, ny = x + dx, y + dy
                if is_valid(nx, ny, n, m) and map_grid[nx][ny] == map_grid[x][y] + 1:
                    queue.append((nx, ny, path + [(nx, ny)]))
        return len(trails)

def sum_of(map_grid, part=1):
    n, m = len(map_grid), len(map_grid[0])
    total = 0
    for i in range(n):
        for j in range(m):
            if map_grid[i][j] == 0:
                total += bfs(map_grid, n, m, i, j, part)
    return total

map_grid = parse('input-1.txt')
print(f"part a: {sum_of(map_grid, 1)}")
print(f"part b: {sum_of(map_grid, 2)}")