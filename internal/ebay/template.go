package ebay

// DefaultDescription is FireCardManager's "Store Branded" listing template
// (DefaultListingTemplates.cs), shared by Sports and TCG: {CardKind},
// {SportLabel} and {TeamLabel} are filled in per edition.
const DefaultDescription = `<div style="max-width:700px;width:100%;margin:0 auto;font-family:Arial,Helvetica,sans-serif;background:#ffffff;">

  <div style="text-align:center;padding:4px 10px 18px 10px;">
    <h1 style="margin:0 0 4px 0;font-size:26px;font-weight:800;letter-spacing:1px;color:#0f172a;text-transform:uppercase;">{StoreName}</h1>
    <p style="margin:0;font-size:13px;color:#64748b;">Carefully Inspected &amp; Securely Shipped {CardKind}</p>
  </div>

  <table width="100%" cellpadding="0" cellspacing="8" style="margin:0 0 10px 0;">
    <tr>
      <td width="50%" style="text-align:center;padding:6px;vertical-align:top;">
        <img src="{FrontImageUrl}" alt="Front of card" style="width:100%;max-width:320px;height:auto;aspect-ratio:5/7;object-fit:cover;border:1px solid #cbd5e1;border-radius:8px;"/>
        <div style="margin-top:4px;font-size:12px;color:#64748b;font-style:italic;">Front</div>
      </td>
      <td width="50%" style="text-align:center;padding:6px;vertical-align:top;">
        <img src="{BackImageUrl}" alt="Back of card" style="width:100%;max-width:320px;height:auto;aspect-ratio:5/7;object-fit:cover;border:1px solid #cbd5e1;border-radius:8px;"/>
        <div style="margin-top:4px;font-size:12px;color:#64748b;font-style:italic;">Back</div>
      </td>
    </tr>
  </table>
  <p style="margin:0 0 18px 0;font-size:12px;color:#b91c1c;font-weight:bold;text-align:center;">The scan above is of the EXACT physical card you will receive &mdash; not a stock photo or a representative image of the set.</p>

  <table width="100%" cellpadding="0" cellspacing="8" style="margin:0 0 22px 0;">
    <tr>
      <td width="33%" style="background-color:#0f766e;border-radius:6px;padding:10px 6px;text-align:center;">
        <div style="color:#ffffff;font-weight:bold;font-size:12px;letter-spacing:0.5px;">30-DAY RETURNS</div>
        <div style="color:#d1fae5;font-size:10px;margin-top:2px;">No questions asked</div>
      </td>
      <td width="33%" style="background-color:#0f766e;border-radius:6px;padding:10px 6px;text-align:center;">
        <div style="color:#ffffff;font-weight:bold;font-size:12px;letter-spacing:0.5px;">FAST, SECURE SHIPPING</div>
        <div style="color:#d1fae5;font-size:10px;margin-top:2px;">Ships promptly, protected</div>
      </td>
      <td width="33%" style="background-color:#0f766e;border-radius:6px;padding:10px 6px;text-align:center;">
        <div style="color:#ffffff;font-weight:bold;font-size:12px;letter-spacing:0.5px;">TRUSTED SELLER</div>
        <div style="color:#d1fae5;font-size:10px;margin-top:2px;">Every card hand-checked</div>
      </td>
    </tr>
  </table>

  <div style="font-size:11px;letter-spacing:1.5px;color:#0f766e;font-weight:bold;text-transform:uppercase;">Details</div>
  <h2 style="margin:2px 0 10px 0;font-size:20px;color:#0f172a;">{Year} {Brand} {SetName} &mdash; {PlayerName} #{CardNumber}</h2>
  <table width="100%" cellpadding="0" cellspacing="0" style="font-size:13px;color:#334155;margin:0 0 6px 0;">
    <tr><td style="padding:4px 8px 4px 0;width:30%;color:#64748b;">{SportLabel}</td><td style="padding:4px 0;font-weight:bold;">{Sport}</td></tr>
    <tr><td style="padding:4px 8px 4px 0;color:#64748b;">{TeamLabel}</td><td style="padding:4px 0;font-weight:bold;">{Team}</td></tr>
    <tr><td style="padding:4px 8px 4px 0;color:#64748b;">Variety</td><td style="padding:4px 0;font-weight:bold;">{Variety}</td></tr>
    <tr><td style="padding:4px 8px 4px 0;color:#64748b;">Condition</td><td style="padding:4px 0;font-weight:bold;">{Condition}</td></tr>
  </table>

  <table width="100%" cellpadding="0" cellspacing="0" style="margin:20px 0 10px 0;"><tr><td style="background:#0f766e;height:3px;line-height:3px;font-size:0;border-radius:2px;">&nbsp;</td></tr></table>
  <div style="font-size:11px;letter-spacing:1.5px;color:#0f766e;font-weight:bold;text-transform:uppercase;">Condition</div>
  <h3 style="margin:2px 0 6px 0;font-size:16px;color:#0f172a;">Raw Condition &mdash; Modern Cards</h3>
  <p style="margin:0;font-size:13px;color:#334155;line-height:1.55;">Unless otherwise noted, raw modern cards are listed in Near Mint or better condition. A Near Mint card is equivalent to a fresh pack &mdash; it has only slight imperfections such as 1&ndash;3 soft or rounded corners and slightly rough edges. Any damage beyond this would lower the card's condition. Centering is clearly visible in the images provided and does not factor into our condition assessment.</p>

  <table width="100%" cellpadding="0" cellspacing="0" style="margin:20px 0 10px 0;"><tr><td style="background:#0f766e;height:3px;line-height:3px;font-size:0;border-radius:2px;">&nbsp;</td></tr></table>
  <div style="font-size:11px;letter-spacing:1.5px;color:#0f766e;font-weight:bold;text-transform:uppercase;">Condition</div>
  <h3 style="margin:2px 0 6px 0;font-size:16px;color:#0f172a;">Raw Condition &mdash; Vintage Cards</h3>
  <p style="margin:0;font-size:13px;color:#334155;line-height:1.55;">Unless otherwise noted, raw vintage cards are listed in Excellent condition or better. An Excellent card has clearly visible signs of wear and may exhibit some of the following: rough edges, moderate chipping, minor discoloration, minor surface indentation, fuzzy corners, or noticeable scratches. Centering is clearly visible in the images provided and does not factor into our condition assessment.</p>

  <table width="100%" cellpadding="0" cellspacing="0" style="margin:20px 0 10px 0;"><tr><td style="background:#0f766e;height:3px;line-height:3px;font-size:0;border-radius:2px;">&nbsp;</td></tr></table>
  <div style="font-size:11px;letter-spacing:1.5px;color:#0f766e;font-weight:bold;text-transform:uppercase;">Shipping</div>
  <h3 style="margin:2px 0 6px 0;font-size:16px;color:#0f172a;">Domestic Shipping</h3>
  <p style="margin:0;font-size:13px;color:#334155;line-height:1.55;">Combined shipping for multiple items is available. If you purchase multiple single items please contact us and we will ship up to 10 cards in a bubble mailer for $4.00 flat rate.<br/>Signature confirmation may be required on invoices over $400. All cards are protected and shipped securely.</p>

  <table width="100%" cellpadding="0" cellspacing="0" style="margin:20px 0 10px 0;"><tr><td style="background:#0f766e;height:3px;line-height:3px;font-size:0;border-radius:2px;">&nbsp;</td></tr></table>
  <div style="font-size:11px;letter-spacing:1.5px;color:#0f766e;font-weight:bold;text-transform:uppercase;">Payment</div>
  <h3 style="margin:2px 0 6px 0;font-size:16px;color:#0f172a;">Payment</h3>
  <p style="margin:0;font-size:13px;color:#334155;line-height:1.55;">{StoreName} utilizes eBay's managed payments system. If you are experiencing difficulties making a payment, please contact eBay for assistance.</p>

  <table width="100%" cellpadding="0" cellspacing="0" style="margin:20px 0 10px 0;"><tr><td style="background:#0f766e;height:3px;line-height:3px;font-size:0;border-radius:2px;">&nbsp;</td></tr></table>
  <div style="font-size:11px;letter-spacing:1.5px;color:#0f766e;font-weight:bold;text-transform:uppercase;">Returns</div>
  <h3 style="margin:2px 0 6px 0;font-size:16px;color:#0f172a;">Return Policy</h3>
  <p style="margin:0;font-size:13px;color:#334155;line-height:1.55;">Returns are accepted for 30 days.</p>

  <table width="100%" cellpadding="0" cellspacing="0" style="margin:20px 0 10px 0;"><tr><td style="background:#0f766e;height:3px;line-height:3px;font-size:0;border-radius:2px;">&nbsp;</td></tr></table>
  <div style="font-size:11px;letter-spacing:1.5px;color:#0f766e;font-weight:bold;text-transform:uppercase;">Disclosure</div>
  <h3 style="margin:2px 0 6px 0;font-size:16px;color:#0f172a;">AI Statement</h3>
  <p style="margin:0 0 22px 0;font-size:13px;color:#334155;line-height:1.55;">Cards have been identified with AI and may contain mistakes regarding card identification. Please examine the images as they are real scans/pictures of the item being offered. Feel free to ask any questions through eBay's messaging system prior to purchase.</p>

  <table width="100%" cellpadding="0" cellspacing="0" style="margin:0;"><tr><td style="background-color:#0f766e;border-radius:8px;padding:22px 16px;text-align:center;">
    <div style="color:#ffffff;font-size:18px;font-weight:800;letter-spacing:1px;text-transform:uppercase;">{StoreName}</div>
    <div style="color:#ccfbf1;font-size:12px;margin-top:4px;">Carefully Inspected &amp; Securely Shipped {CardKind}</div>
    <div style="color:#ffffff;font-size:12px;margin-top:14px;">
      <span style="margin:0 10px;">&#9889; Fast Dispatch</span>
      <span style="margin:0 10px;">&#10003; Trusted Seller</span>
      <span style="margin:0 10px;">&#8617; Easy Returns</span>
    </div>
    <div style="color:#99f6e4;font-size:10px;margin-top:12px;">Copyright &copy; {StoreName}</div>
  </td></tr></table>

</div>
`
