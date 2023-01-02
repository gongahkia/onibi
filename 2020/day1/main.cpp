#include <iostream>
#include <fstream>
#include <vector>

using namespace std;

int main() {
	
	int sum;
	int condition = 0;
	string line;
	fstream fhand;
	vector <int> numholder;
	fhand.open("input.txt",ios::in);
	
    //create the vector to store all values
    if (fhand.is_open()) {
        while (getline(fhand,line)) {
            numholder.push_back(stoi(line));
        }
        fhand.close();
    }

    for (int i = 0; i < numholder.size(); i++) {
        sum = 2020 - numholder[i];
        for (int q = 0; q < numholder.size(); q++) {
            if (numholder[q] == sum) {
                cout << "part 1: " << sum * (2020-sum) << endl;
                break;}
        }
    }

	for (int i: numholder) {
		for (int q: numholder) {
			for (int k: numholder) {
				sum = i + q + k;
				if (sum == 2020) {
					cout << "part2: " << i * q * k << endl;
					break;
			} else {
				sum = 0;
			}
	}
			if (sum == 2020) {
				break;
			}
	}
		if (sum == 2020) {
			break;
		}
	}
	return 0;
}
