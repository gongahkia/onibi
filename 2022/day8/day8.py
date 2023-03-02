fhand = open("input2.txt", "r")

print("\n")

row_collector = {}
row_num = 1

column_collector = {}
column_num = 1

for line in fhand:
    print(line, end="")

# to create the row
    row_collector[row_num] = line.rstrip("\n")
    row_num += 1

# to create the column
for row in row_collector:
    column_num = 1
    for tree in row_collector[row]:
        column_collector[column_num] = column_collector.get(column_num, "") + tree
        column_num += 1

print("\n\n--------------------\n")

print(f"columns: {column_collector}")
print(f"rows: {row_collector}\n")

total_left_count = 0
total_right_count = 0
top_count = 0
bottom_count = 0

for row in row_collector:
    left_count = 1
    for itervar in range(len(row_collector[row])):
        if row_collector[row][itervar+1] <= row_collector[row][itervar]:
            break
        else:
            left_count += 1
    total_left_count += left_count

for row in row_collector:
    right_count = 1
    for itervar in range(len(row_collector[row])):
        if row_collector[row][len(row_collector)[row]-itervar] <= row_collector[row][len(row_collector)[row]-itervar-1]:
            break
        else:
            right_count += 1
    total_right_count += right_count

print(f"left count: {total_left_count}")