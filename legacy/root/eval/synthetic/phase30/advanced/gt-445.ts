const wallet = {
  credit(_user: string, _amount: number) {
  },
};

export function redeemGiftCard(req: { user: string; body: { cardId: string; amount: number } }) {
  const amount = req.body.amount;
  wallet.credit(req.user, amount); // sink
  return { cardId: req.body.cardId, amount };
}
