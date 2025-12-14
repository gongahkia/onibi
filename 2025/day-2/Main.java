
import java.io.BufferedReader;
import java.io.FileReader;
import java.io.IOException;
import java.util.List;

public class Main {
    public static void main(String[] args){
        String fileName = "input.txt";
        try (BufferedReader reader = new BufferedReader(new FileReader(fileName))){ 
            String line; 
            while ((line = reader.readLine()) != null) {
                // System.out.println(line);
                String[] patterns = line.split(",");
                for(String stringVar: patterns) {
                    // System.out.println(stringVar);
                    String[] indivPattern = stringVar.split("-");
                    Integer startingInt = Integer.parseInt(indivPattern[0]);
                    Integer endingInt = Integer.parseInt(indivPattern[1]);
                    System.out.println("Starting number: " + String.valueOf(startingInt) + " Ending number: " + String.valueOf(endingInt));
                }
                // System.out.println("done iterating");
            }
        } catch (IOException caughtError) { 
            System.out.println(caughtError.getMessage()); 
        }
    }
}
