import jakarta.servlet.http.HttpServlet;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import jakarta.servlet.annotation.WebServlet;

@WebServlet("/hello")
class Gt418Servlet extends HttpServlet {
    protected void doGet(HttpServletRequest req, HttpServletResponse resp) throws java.io.IOException {
        String name = req.getParameter("name");
        resp.setContentType("text/html");
        resp.getWriter().write("<h1>Hello " + name + "</h1>"); // sink
    }
}
