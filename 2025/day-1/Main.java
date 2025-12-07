import java.io.BufferedReader;
import java.io.FileReader;
import java.io.IOException;

public class Main {
    public static void main(String[] args){
        String fileName = "input.txt";
        try (BufferedReader reader = new BufferedReader(new FileReader(fileName))){ // create buffered reader that reads from filereader 
            String line; 
            Integer passwordOne = 0;
            Integer passwordTwo = 0;
            Integer currentPosition = 50;
            while ((line = reader.readLine()) != null) {
                int steps = Integer.parseInt(line.substring(1, line.length()));
                int zeroCrossings = 0;
                if (line.startsWith("L")){
                    if (currentPosition == 0) {
                        zeroCrossings = steps / 100;
                    } else if (currentPosition <= steps) {
                        zeroCrossings = (steps - currentPosition) / 100 + 1;
                    }
                    currentPosition = (currentPosition - steps % 100 + 100) % 100;
                } else if (line.startsWith("R")){
                    if (currentPosition == 0) {
                        zeroCrossings = steps / 100;
                    } else if (100 - currentPosition <= steps) {
                        zeroCrossings = (steps - (100 - currentPosition)) / 100 + 1;
                    }
                    currentPosition = (currentPosition + steps) % 100;
                } else {
                    System.out.println("line started with unknown character" + line);
                }

                passwordTwo += zeroCrossings;
                System.out.println("instruction line: " + line + " and the current number: " + currentPosition + " (crossed 0: " + zeroCrossings + " times)");

                if (currentPosition == 0) {
                    passwordOne += 1;
                }
            }
            System.out.println(passwordOne);
            System.out.println(passwordTwo);

        } catch (IOException caughtError) { // also note that we force a try and catch block here since these are checked exceptions that java forces us to check
            System.out.println(caughtError.getMessage()); // print error if real
        }
    }
}
