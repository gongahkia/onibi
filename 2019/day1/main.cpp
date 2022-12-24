#include <iostream>
#include <cmath>
#include <fstream>
 
using namespace std;


// to parse each individual line of the string
int main () {
    string line;
    int part1totalsum = 0;
    int tempsum;
    ifstream fhand ("input.txt");
  
    while (getline (fhand, line)) {
        // PART 1
        tempsum = 0;
        tempsum = floor(stoi(line)/3)-2;
        part1totalsum += tempsum;
    };
    cout << "Part 1: " << part1totalsum << endl;
}

   
