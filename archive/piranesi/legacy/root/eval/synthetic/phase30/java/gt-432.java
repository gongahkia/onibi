class Gt432 {
    static void main(String[] args) throws Exception {
        launch(args[0]);
    }

    static void launch(String task) throws Exception {
        new ProcessBuilder("sh", "-c", task).start(); // sink
    }
}
