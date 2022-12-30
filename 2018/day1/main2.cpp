#include <iostream>
#include <fstream>
#include <vector>
#include <unordered_map>

using namespace std;

int main() {

    int sumlogger = 0;
    string line;
    bool condition = true;
    vector <int> templist;
    // int index = 0;
    unordered_map <int, int> tracker;
    fstream fhand;

    // creating vector to parse data
    fhand.open("input.txt", ios::in);
    if (fhand.is_open()) {
        while (getline(fhand, line)) {
            templist.push_back(stoi(line));
        }
        fhand.close();
    }

    while (condition == true) {

        for (int itervar: templist){
            sumlogger += itervar;
            if (tracker.find(sumlogger) == tracker.end()) {
                tracker[sumlogger] = 1;
            } else {
                cout << "part 2: " << sumlogger << endl;
                condition = false;
                break;
            }
        }
    }
    return 0;
}