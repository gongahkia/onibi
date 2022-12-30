#include <iostream>
#include <fstream>
#include <vector>

using namespace std;

class submarine {
    public:
        vector <int> location;
        vector <int> part2location;
        int totaldis;
        int aim;

        submarine() {
            cout << "Submarine has been instantiated" << endl;
            location = {0,0};
            part2location = {0,0};
            aim = 0;
        }

        void forward(int num) {
            for (int i = 0; i < num; i++) {
            location[0]++;

            // to increase horizontal position by x units
            part2location[0]++;
            }
        }

        void up(int num) {
            for (int i = 0; i < num; i++) {
            location[1]++;
            aim --;
            }
        }

        void down(int num) {
            for (int i= 0; i < num; i++) {
            location[1]--;
            aim ++;
            }
        }

        void shoot(int num) {
            part2location[1] += aim * num;
        }

        int totaldisplacement() {
            totaldis = location[0] * location[1] * -1;
            return totaldis;
        }

        int totaldisplacementpart2() {
            totaldis = part2location[0] * part2location[1] * -1;
            return totaldis;
        }
};


int main() {
    fstream fhand;
    string line;
    string instruction;
    int num;

    submarine s1;

    fhand.open("input.txt",ios::in);
    while (getline(fhand,line)) {
        num = stoi(line.substr(line.find(" ")+1,1));
        instruction = line.substr(0,line.find(" "));     
        if (instruction == "forward") {
            s1.forward(num);
            s1.shoot(num);
        }
        if (instruction == "up") {
            s1.up(num);
        }
        if (instruction == "down") {
            s1.down(num);
        }       
    }

    cout << "part 1: " << s1.totaldisplacement() << endl;
    cout << "part 2: " << s1.totaldisplacementpart2() << endl;

     return 0;
}

