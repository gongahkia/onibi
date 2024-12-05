from collections import defaultdict, deque

def parse(target_filepath):
    with open(target_filepath, "r") as f:
        lines = f.read().splitlines()
    divider_index = lines.index("")
    rules = lines[:divider_index]
    updates = lines[divider_index + 1:]
    rule_array = []
    for rule in rules:
        x, y = map(int, rule.split("|"))
        rule_array.append((x, y))
    update_array = []
    for update in updates:
        update_array.append(list(map(int, update.split(","))))
    return rule_array, update_array

def valid(rules, update):
    update_set = set(update)
    for x, y in rules:
        if x in update_set and y in update_set:
            if update.index(x) > update.index(y):
                return False
    return True

def week11_topological_sort_thank_you_smu(rules, update):
    graph = defaultdict(list)
    in_deg = defaultdict(int)
    for x, y in rules:
        if x in update and y in update:
            graph[x].append(y)
            in_deg[y] += 1
            if x not in in_deg:
                in_deg[x] = 0
    queue = deque([node for node in update if in_deg[node] == 0])
    sorted_order = []
    while queue:
        node = queue.popleft()
        sorted_order.append(node)
        for neighbor in graph[node]:
            in_deg[neighbor] -= 1
            if in_deg[neighbor] == 0:
                queue.append(neighbor)
    if len(sorted_order) != len(update):
        return None  # cycle so cannot reorder
    return sorted_order

def mid_sum(target_filepath):
    rules, updates = parse(target_filepath)
    valid_updates = [update for update in updates if valid(rules, update)]
    part_1_result = sum(update[len(update)//2] for update in valid_updates)
    part_2_result = 0
    for update in updates:
        if not valid(rules, update):
            ordered_update = week11_topological_sort_thank_you_smu(rules, update)
            if ordered_update:
                part_2_result += ordered_update[len(ordered_update)//2]
    return part_1_result, part_2_result

if __name__ == "__main__":
    part_1_result, part_2_result = mid_sum("input-1.txt")
    print(f'part a: {part_1_result}')
    print(f'part b: {part_2_result}')