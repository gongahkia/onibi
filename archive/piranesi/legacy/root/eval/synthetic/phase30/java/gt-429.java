class Gt429 {
    static void main(String[] args) throws Exception {
        runCommand(args[0]);
    }

    static void runCommand(String command) throws Exception {
        Runtime.getRuntime().exec(command); // sink
    }
}
