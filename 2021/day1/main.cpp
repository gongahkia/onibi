#include <iostream>
#include <fstream>
#include <cmath>

using namespace std;

int main() {
    string line;
    int prevnum = 0;
    int counter = 0;

    fstream fhand;
    fhand.open("input.txt", ios::in);
    if (fhand.is_open()) {
        while (getline(fhand,line)) {
            if (prevnum == 0) {
                prevnum = stoi(line);
            } else if (stoi(line) > prevnum) {
                counter ++;
                prevnum = stoi(line);
            } else {
                prevnum = stoi(line);
            }
        }  
    }
    fhand.close();

    cout << "part 1 measurement count: " << counter;
    
    return 0;
}