#include <iostream>
#include <cmath>
#include <fstream>

using namespace std;

int main () {
    string line;
    int totalsum = 0;
    int cumsum;
    int tempsum;
    
    ifstream fhand ("input.txt");

    while (getline(fhand, line)) {
        cumsum = 0;
        tempsum = floor(stoi(line)/3)-2;
        while (tempsum > 0) {
            cumsum += tempsum;
            tempsum = floor(tempsum/3)-2;
        }
        totalsum += cumsum;
    };

    cout << "part 2: " << totalsum << endl;

    return 0;
}