from itertools import product

def lr_eval_simple(num_array, operators):
    res = num_array[0]
    for i, op in enumerate(operators):
        if op == '+':
            res += num_array[i + 1]
        elif op == '*':
            res *= num_array[i + 1]
    return res

def lr_eval_complex(numbers, operators):
    result = numbers[0]
    for i, op in enumerate(operators):
        if op == '+':
            result += numbers[i + 1]
        elif op == '*':
            result *= numbers[i + 1]
        elif op == '||':
            result = int(str(result) + str(numbers[i + 1]))
    return result

def valid_eqns(file_path):
    calibration = 0
    try:
        with open(file_path, 'r') as file:
            lines = file.readlines()
        for line in lines:
            test_val, num_str = line.split(":")
            test_val = int(test_val.strip())
            num_array = list(map(int, num_str.strip().split()))
            count = len(num_array)
            operator_combinations = product(['+', '*'], repeat=count - 1)
            valid = False
            for operators in operator_combinations:
                if lr_eval_simple(num_array, operators) == test_val:
                    valid = True
                    break
            if valid:
                calibration += test_val
        return calibration
    except FileNotFoundError:
        return None

def valid_concatenated_eqns(file_path):
    calibration = 0
    try:
        with open(file_path, 'r') as file:
            lines = file.readlines()
        for line in lines:
            test_val, num_str= line.split(":")
            test_val = int(test_val.strip())
            num_array = list(map(int, num_str.strip().split()))
            num_count = len(num_array)
            operator_combinations = product(['+', '*', '||'], repeat=num_count - 1)
            valid = False
            for operators in operator_combinations:
                if lr_eval_complex(num_array, operators) == test_val:
                    valid = True
                    break
            if valid:
                calibration += test_val
        return calibration
    except FileNotFoundError:
        return None

print(f'part a: {valid_eqns("input-1.txt")}')
print(f'part b: {valid_concatenated_eqns("input-1.txt")}')