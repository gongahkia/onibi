# ----- helper functions -----

def debug(fileName:str):
    print("-----")
    fhand = open(fileName)
    for l in fhand:
        print(l)
    print("-----")

def read_data_a(fileName:str):
    fhand = open(fileName)
    tem1 = []
    tem2 = []
    for line in fhand:
        if line.startswith("Time"):
            for item in line.split(" "):
                if item.strip() != "" and item.strip() != "Time:":
                    tem1.append(item.strip())
        elif line.startswith("Distance"):
            for item in line.split(" "):
                if item.strip() != "" and item.strip() != "Distance:":
                    tem2.append(item.strip())
        else:
            print("this edge case should never be hit")
    fhand.close()

    assert(len(tem1) == len(tem2)) # ensure same length and no issue hit

    return (tem1, tem2)

def read_data_b(fileName:str):
    fhand = open(fileName)
    tem1 = []
    tem2 = []
    for line in fhand:
        if line.startswith("Time"):
            for item in line.split(" "):
                if item.strip() != "" and item.strip() != "Time:":
                    tem1.append(item.strip())
        elif line.startswith("Distance"):
            for item in line.split(" "):
                if item.strip() != "" and item.strip() != "Distance:":
                    tem2.append(item.strip())
        else:
            print("this edge case should never be hit")

    assert(len(tem1) == len(tem2)) # ensure same length and no issue hit

    fhand.close()

    return (["".join(tem1)],["".join(tem2)])

def sol(fin):
    print(fin)
    fin2 = []
    for i in range(len(fin[0])):
        time = int(fin[0][i])
        recordDistance = int(fin[1][i])
        count = 0
        for chargeTime in range(0, time+1):
            travelTime = time - chargeTime
            speed = chargeTime
            travelDistance = speed * travelTime
            # print(f"charge time: {chargeTime}, travel time: {travelTime}, speed: {speed}, travel distance = {travelDistance}")
            if travelDistance > recordDistance:
                count += 1
        fin2.append(count)
    print(fin2)
    fin3 = 1
    for q in fin2:
        fin3 *= q
    return fin3

# ----- actual execution code -----

fileName = "input.txt"
print(f"part a: {sol(read_data_a(fileName))}")
print(f"part b: {sol(read_data_b(fileName))}")
