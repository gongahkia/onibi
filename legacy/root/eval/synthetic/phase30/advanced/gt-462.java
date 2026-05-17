import jakarta.servlet.annotation.WebServlet;
import jakarta.servlet.http.HttpServlet;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;

@WebServlet("/verify")
class Gt462Servlet extends HttpServlet {
    protected void doPost(HttpServletRequest req, HttpServletResponse resp) throws java.io.IOException {
        String expected = lookupToken(req.getParameter("user"));
        String provided = req.getParameter("token");
        if (expected.equals(provided)) { // sink
            resp.getWriter().write("ok");
        }
    }

    String lookupToken(String user) {
        return user + "-token";
    }
}
