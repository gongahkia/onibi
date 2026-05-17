import jakarta.servlet.annotation.WebServlet;
import jakarta.servlet.http.HttpServlet;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;

@WebServlet("/expedite")
class Gt446Servlet extends HttpServlet {
    private final Gt446Orders orders = new Gt446Orders();

    protected void doPost(HttpServletRequest req, HttpServletResponse resp) {
        boolean paid = Boolean.parseBoolean(req.getParameter("paid"));
        if (paid) {
            orders.markExpedited(req.getParameter("orderId")); // sink
        }
    }
}

class Gt446Orders {
    void markExpedited(String orderId) {
    }
}
