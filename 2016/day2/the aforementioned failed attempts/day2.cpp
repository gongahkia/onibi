#include <iostream>
#include <fstream>

using namespace std;

class movement:

    public:
        



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
