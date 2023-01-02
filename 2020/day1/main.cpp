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
	
	if (fhand.is_open()) {
		while (getline(fhand,line)) {
			numholder.push_back(stoi(line));
		}
		fhand.close();
	}

	for (int a: numholder) {
		for (int b: numholder) {
				sum = a + b;
				if (sum == 2020) {
				cout << "part1: " << a * b << endl;
				break;
				} else {
				sum = 0;
				}
		}
				if (sum == 2020) {
				break;
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
