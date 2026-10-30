// Load test for the read paths: filtered list, pagination and aging stock.
//
//   make seed-load                                   # 250k vehicles
//   RATE_LIMIT_RPS=0 docker compose up -d api        # one client IP, so no rate limit
//   TOKEN=$(docker compose exec -T api /api token -role viewer)
//   docker run --rm --network host -e TOKEN=$TOKEN -v "$PWD/scripts/k6:/k6" \
//     grafana/k6:1.4.0 run /k6/list.js
//
// Target: p95 < 100 ms with 50 VUs for 1 minute.
import http from "k6/http";
import { check } from "k6";

const BASE = __ENV.BASE_URL || "http://localhost:8080";
const params = {
  headers: { Authorization: `Bearer ${__ENV.TOKEN}` },
  tags: {},
};

export const options = {
  vus: 50,
  duration: "1m",
  thresholds: {
    http_req_duration: ["p(95)<100"],
    http_req_failed: ["rate<0.01"],
    checks: ["rate>0.99"],
  },
};

const makes = ["Toyota", "Honda", "Ford", "Chevrolet", "Nissan", "Hyundai", "Kia", "Subaru"];
const models = { Toyota: "Camry", Honda: "Civic", Ford: "F-150", Chevrolet: "Equinox", Nissan: "Rogue", Hyundai: "Tucson", Kia: "Sportage", Subaru: "Outback" };
const pick = (a) => a[Math.floor(Math.random() * a.length)];

function get(name, path) {
  const res = http.get(`${BASE}${path}`, { headers: params.headers, tags: { name } });
  check(res, { [`${name} 200`]: (r) => r.status === 200 });
  return res;
}

export default function () {
  const make = pick(makes);
  switch (Math.floor(Math.random() * 6)) {
    case 0:
      get("list", "/v1/vehicles?limit=50");
      break;
    case 1:
      get("list_make_model", `/v1/vehicles?make=${make}&model=${encodeURIComponent(models[make])}&limit=50`);
      break;
    case 2: {
      const min = Math.floor(Math.random() * 200);
      get("list_age_range", `/v1/vehicles?min_age_days=${min}&max_age_days=${min + 30}&limit=50`);
      break;
    }
    case 3:
      get("aging", "/v1/vehicles/aging?limit=50");
      break;
    case 4:
      get("aging_make", `/v1/vehicles/aging?make=${make}&limit=50`);
      break;
    case 5: {
      // Follow the cursor a few pages deep into the aging list.
      let res = get("aging_page", "/v1/vehicles/aging?limit=100");
      for (let i = 0; i < 3 && res.status === 200; i++) {
        const cur = res.json("next_cursor");
        if (!cur) break;
        res = get("aging_page", `/v1/vehicles/aging?limit=100&cursor=${cur}`);
      }
      break;
    }
  }
}
