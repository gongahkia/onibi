import org.springframework.web.bind.annotation.PostMapping;

public class PaymentController {
    @PostMapping("/charge")
    public void chargeCard() {
        processPayment();
    }
}
