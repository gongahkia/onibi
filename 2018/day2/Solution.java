import java.util.*;
import java.io.File;
// implement another solution that involves import java.util.Scanner

public class Solution {

    public static void main(String[] args) {
        try {
            FileReader fhand = new FileReader("input.txt");
            int data = fhand.read();

            while (data != -1) {
                if (data == 10) {
                    System.out.print("\n");
                } else {
                    System.out.print((char) data);
                }
                data = fhand.read();
            }
        } catch (FileNotFoundException e) {
            e.printStackTrace();
        } catch (IOException e) {
            e.printStackTrace();
        }
        fhand.close();
    }

}