#include <iostream>
#include <fstream>
// #include <typeinfo> -> used to check on the type of value after stoi() converted string to int

using namespace std;

int main() {
    fstream fhand;
    string line;
    int sum = 0;
    int newval;
    vector <int> numcounter;

    fhand.open("input.txt", ios::in);

    numcounter.push_back(0);

    if (fhand.is_open()) {
        while (getline(fhand, line)) {
            newval = stoi(line);
            if (sum == 0) {
                sum = newval;
            } else {
                sum += newval;
            }
        }
    }

    fhand.close();

    cout << "part 1: " << sum << endl;
    return 0;
}
