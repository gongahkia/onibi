class Gt430 {
    static void main(String[] args) throws Exception {
        String command = args[0];
        new ProcessBuilder("sh", "-c", command).start(); // sink
    }
}
