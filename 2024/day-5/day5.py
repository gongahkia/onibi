"""
i've caved, its python time baby
"""

def parse(file_path):
    with open(file_path, "r") as f:
        lines = f.read().splitlines()
    divider_index = lines.index("")
    rules = lines[:divider_index]
    updates = lines[divider_index + 1:]
    parsed_rules = []
    for rule in rules:
        x, y = map(int, rule.split("|"))
        parsed_rules.append((x, y))
    parsed_updates = []
    for update in updates:
        parsed_updates.append(list(map(int, update.split(","))))
    return parsed_rules, parsed_updates

def valid(rules, update):
    update_set = set(update)
    for x, y in rules:
        if x in update_set and y in update_set:
            if update.index(x) > update.index(y):
                return False
    return True

def mid_sum(file_path):
    rules, updates = parse(file_path)
    valid_updates = [update for update in updates if valid(rules, update)]
    return sum(update[len(update)//2] for update in valid_updates)

if __name__ == "__main__":
    print(f'part a: {mid_sum("input-1.txt")}')
