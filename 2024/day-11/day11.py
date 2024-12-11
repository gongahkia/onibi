def parse(target_filepath):
    with open(target_filepath, 'r') as file:
        content = file.read()
    return content.split()  

def transform_stones(stones):
    new_stones = []
    for stone in stones:
        if stone == '0':
            new_stones.append('1')
        elif len(stone) % 2 == 0:  
            mid = len(stone) // 2
            left = stone[:mid].lstrip('0') or '0'
            right = stone[mid:].lstrip('0') or '0'
            new_stones.append(left)
            new_stones.append(right)
        else:  
            new_stones.append(str(int(stone) * 2024))
    return new_stones

def blink(stones, times):
    for _ in range(times):
        stones = transform_stones(stones)
    return stones

initial_stones = parse("input-1.txt")
resulting_stones = blink(initial_stones, 25)
print(f'part a: {len(resulting_stones)}')  
# resulting_stones = blink(initial_stones, 75)
# print(f'part b: {len(resulting_stones)}')  