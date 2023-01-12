#include <iostream>
#include <fstream>

using namespace std;

class movement:

    public:
    // something class object something method something movement

int main () {

    fstream fhand;
    string line;

    fhand.open("input.txt",ios::in);
    if (fhand.is_open()) {
        while (getline(fhand,line)) {
            cout << line << endl;
        }
    }
    //something something spaghetti code
    return 0;
}
