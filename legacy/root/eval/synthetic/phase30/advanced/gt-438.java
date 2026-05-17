import jakarta.servlet.annotation.WebServlet;
import jakarta.servlet.http.HttpServlet;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import jakarta.servlet.http.HttpSession;

@WebServlet("/redeem")
class Gt438Servlet extends HttpServlet {
    protected void doPost(HttpServletRequest req, HttpServletResponse resp) {
        HttpSession session = req.getSession();
        Boolean redeemed = (Boolean) session.getAttribute("coupon_redeemed");
        String code = req.getParameter("coupon");
        if (redeemed == null || !redeemed) {
            session.setAttribute("coupon_redeemed", true);
            applyCoupon(code); // sink
        }
    }

    void applyCoupon(String code) {
    }
}
